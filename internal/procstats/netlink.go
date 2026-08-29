package procstats

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/netip"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	nlmsgHeaderSize  = 16
	inetDiagReqSize  = 56
	inetDiagMsgSize  = 72
	inetDiagInfoAttr = 2
	inetDiagNoCookie = ^uint32(0)
	tcpProtocol      = 6
)

func (socket diagSocket) key() socketKey {
	return socketKey{cookie: socket.cookie, inode: socket.inode}
}

func dumpTCPFamily(ctx context.Context, family uint8) ([]diagSocket, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM, unix.NETLINK_INET_DIAG)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, err
	}
	address := &unix.SockaddrNetlink{Family: unix.AF_NETLINK}
	if err := unix.Bind(fd, address); err != nil {
		return nil, err
	}
	request := makeDiagRequest(family)
	if err := unix.Sendto(fd, request, 0, address); err != nil {
		return nil, err
	}

	result := make([]diagSocket, 0)
	buffer := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		if _, err := unix.Poll(poll, 100); err != nil {
			if err == unix.EINTR {
				continue
			}
			return nil, err
		}
		if poll[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			soError, socketErr := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ERROR)
			if socketErr == nil && soError != 0 {
				return nil, unix.Errno(soError)
			}
			return nil, fmt.Errorf("inet_diag socket poll error: revents=0x%x", poll[0].Revents)
		}
		if poll[0].Revents&unix.POLLIN == 0 {
			continue
		}
		n, _, err := unix.Recvfrom(fd, buffer, 0)
		if err != nil {
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK || err == unix.EINTR {
				continue
			}
			return nil, err
		}
		messages, done, err := parseNetlinkMessages(buffer[:n], family)
		if err != nil {
			return nil, err
		}
		result = append(result, messages...)
		if done {
			return result, nil
		}
	}
}

func makeDiagRequest(family uint8) []byte {
	messageLength := nlmsgHeaderSize + inetDiagReqSize
	message := make([]byte, messageLength)
	native := binary.NativeEndian
	native.PutUint32(message[0:4], uint32(messageLength))
	// Netlink's type and flags occupy separate header fields. The kernel
	// expects SOCK_DIAG_BY_FAMILY as the message type and the dump/request
	// bits in nlmsg_flags; mixing them makes the request silently wait forever.
	native.PutUint16(message[4:6], unix.SOCK_DIAG_BY_FAMILY)
	native.PutUint16(message[6:8], uint16(unix.NLM_F_REQUEST|unix.NLM_F_DUMP))
	native.PutUint32(message[8:12], 1)
	payload := message[nlmsgHeaderSize:]
	payload[0] = family
	payload[1] = tcpProtocol
	// Request INET_DIAG_INFO, which contains Linux's tcp_info counters.
	payload[2] = 1 << (inetDiagInfoAttr - 1)
	native.PutUint32(payload[4:8], ^uint32(0))
	for offset := 48; offset < 56; offset += 4 {
		native.PutUint32(payload[offset:offset+4], inetDiagNoCookie)
	}
	return message
}

func parseNetlinkMessages(data []byte, family uint8) ([]diagSocket, bool, error) {
	result := make([]diagSocket, 0)
	for len(data) >= nlmsgHeaderSize {
		native := binary.NativeEndian
		length := int(native.Uint32(data[0:4]))
		if length < nlmsgHeaderSize || length > len(data) {
			return nil, false, fmt.Errorf("invalid netlink message length %d", length)
		}
		typeID := native.Uint16(data[4:6])
		flags := native.Uint16(data[6:8])
		payload := data[nlmsgHeaderSize:length]
		switch typeID {
		case unix.NLMSG_DONE:
			if flags&unix.NLM_F_DUMP_INTR != 0 {
				return nil, false, fmt.Errorf("inet_diag dump interrupted by the kernel")
			}
			return result, true, nil
		case unix.NLMSG_ERROR:
			if len(payload) < 4 {
				return nil, false, fmt.Errorf("short netlink error message")
			}
			code := int32(native.Uint32(payload[:4]))
			if code != 0 {
				return nil, false, syscall.Errno(-code)
			}
		default:
			if socket, ok := parseDiagSocket(payload, family); ok {
				result = append(result, socket)
			}
		}
		aligned := (length + 3) &^ 3
		if aligned > len(data) {
			return nil, false, fmt.Errorf("invalid aligned netlink message length %d", length)
		}
		data = data[aligned:]
	}
	return result, false, nil
}

func parseDiagSocket(payload []byte, family uint8) (diagSocket, bool) {
	if len(payload) < inetDiagMsgSize {
		return diagSocket{}, false
	}
	native := binary.NativeEndian
	if payload[0] != family {
		return diagSocket{}, false
	}
	socket := diagSocket{
		ifIndex: int(native.Uint32(payload[40:44])),
		inode:   uint64(native.Uint32(payload[68:72])),
		cookie: [2]uint32{
			native.Uint32(payload[44:48]),
			native.Uint32(payload[48:52]),
		},
	}
	// TIME_WAIT and other already-detached kernel sockets commonly have no
	// inode and therefore cannot have a userspace owner. Keeping them would
	// create a misleading "unknown process" row with no attributable traffic.
	if socket.inode == 0 {
		return diagSocket{}, false
	}
	socket.local = parseDiagAddress(payload[4+4:4+20], family)
	socket.remote = parseDiagAddress(payload[4+20:4+36], family)
	attributes := payload[inetDiagMsgSize:]
	for len(attributes) >= 4 {
		length := int(native.Uint16(attributes[0:2]))
		if length < 4 || length > len(attributes) {
			break
		}
		attributeType := native.Uint16(attributes[2:4]) & 0x3fff
		if attributeType == inetDiagInfoAttr {
			info := attributes[4:length]
			if len(info) >= 136 {
				socket.rxBytes = native.Uint64(info[128:136])
			}
			if len(info) >= 208 {
				socket.txBytes = native.Uint64(info[200:208])
			}
		}
		aligned := (length + 3) &^ 3
		if aligned > len(attributes) {
			break
		}
		attributes = attributes[aligned:]
	}
	return socket, true
}

func parseDiagAddress(data []byte, family uint8) netip.Addr {
	if family == unix.AF_INET && len(data) >= 4 {
		return netip.AddrFrom4([4]byte{data[0], data[1], data[2], data[3]})
	}
	if family == unix.AF_INET6 && len(data) >= 16 {
		var address [16]byte
		copy(address[:], data[:16])
		return netip.AddrFrom16(address)
	}
	return netip.Addr{}
}
