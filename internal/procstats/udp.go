package procstats

import (
	"context"
	"encoding/binary"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	udpProtocol         = 17
	etherTypeIPv4       = 0x0800
	etherTypeIPv6       = 0x86dd
	etherTypeVLAN       = 0x8100
	etherTypeVLAN8021AD = 0x88a8
	etherTypeVLAN9100   = 0x9100
)

type udpPacket struct {
	family          uint8
	source          netip.Addr
	destination     netip.Addr
	sourcePort      uint16
	destinationPort uint16
	payload         uint64
}

func readProcUDPSockets(ctx context.Context, root string) ([]udpSocket, error) {
	result := make([]udpSocket, 0)
	for _, item := range []struct {
		path   string
		family uint8
	}{
		{path: filepath.Join(root, "net", "udp"), family: unix.AF_INET},
		{path: filepath.Join(root, "net", "udp6"), family: unix.AF_INET6},
	} {
		file, err := openProcSocketTable(item.path)
		if err != nil {
			return nil, err
		}
		if file == nil {
			continue
		}
		lines, err := readLines(file)
		file.Close()
		if err != nil {
			return nil, err
		}
		for _, line := range lines[1:] {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if socket, ok := parseProcUDPSocketLine(line, item.family); ok {
				result = append(result, socket)
			}
		}
	}
	return result, nil
}

func openProcSocketTable(path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil && os.IsNotExist(err) {
		return nil, nil
	}
	return file, err
}

func parseProcUDPSocketLine(line string, family uint8) (udpSocket, bool) {
	fields := strings.Fields(line)
	if len(fields) < 10 {
		return udpSocket{}, false
	}
	local, localPort, ok := parseProcEndpointPort(fields[1], family)
	if !ok {
		return udpSocket{}, false
	}
	remote, remotePort, ok := parseProcEndpointPort(fields[2], family)
	if !ok {
		return udpSocket{}, false
	}
	inode, err := strconv.ParseUint(fields[9], 10, 64)
	if err != nil || inode == 0 {
		return udpSocket{}, false
	}
	return udpSocket{
		inode: inode, family: family,
		local: local, remote: remote,
		localPort: localPort, remotePort: remotePort,
	}, true
}

type udpCapture struct {
	fd     int
	buffer []byte
}

func newUDPCapture() (*udpCapture, error) {
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(uint16(unix.ETH_P_ALL))))
	if err != nil {
		return nil, err
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return &udpCapture{fd: fd, buffer: make([]byte, 64*1024)}, nil
}

func (capture *udpCapture) close() error {
	if capture == nil || capture.fd < 0 {
		return nil
	}
	err := unix.Close(capture.fd)
	capture.fd = -1
	return err
}

func (capture *udpCapture) collect(ctx context.Context, sockets []udpSocket, owners map[uint64]processOwner, resolver interfaceResolver) (map[processKey]udpTraffic, error) {
	traffic := make(map[processKey]udpTraffic)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, address, err := unix.Recvfrom(capture.fd, capture.buffer, 0)
		if err != nil {
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				return traffic, nil
			}
			if err == unix.EINTR {
				continue
			}
			return nil, err
		}
		packet, ok := parseUDPPacket(capture.buffer[:n])
		if !ok || packet.payload == 0 {
			continue
		}
		ifIndex := 0
		if link, ok := address.(*unix.SockaddrLinklayer); ok {
			ifIndex = link.Ifindex
		}
		direction := -1
		if link, ok := address.(*unix.SockaddrLinklayer); ok && link.Pkttype == unix.PACKET_OUTGOING {
			direction = 1
		} else if link, ok := address.(*unix.SockaddrLinklayer); ok && link.Pkttype == unix.PACKET_HOST {
			direction = 0
		}
		socket, outbound, ok := matchUDPPacket(packet, sockets, owners, direction)
		if !ok {
			continue
		}
		owner, found := owners[socket.inode]
		if !found {
			owner = processOwner{name: "unknown"}
		}
		interfaceName := resolver.resolveValues(ifIndex, packet.source, packet.destination)
		if interfaceName == "" {
			interfaceName = resolver.resolveUDP(socket)
		}
		key := processKey{pid: owner.pid, startTime: owner.startTime, interfaceName: interfaceName}
		observed := traffic[key]
		observed.inode = socket.inode
		if outbound {
			observed.tx = saturatingAdd(observed.tx, packet.payload)
		} else {
			observed.rx = saturatingAdd(observed.rx, packet.payload)
		}
		traffic[key] = observed
	}
}

func matchUDPPacket(packet udpPacket, sockets []udpSocket, owners map[uint64]processOwner, direction int) (udpSocket, bool, bool) {
	bestScore := -1
	bestPID := int(^uint(0) >> 1)
	var best udpSocket
	var bestOutbound bool
	for _, socket := range sockets {
		outbound, score, ok := matchUDPSocket(packet, socket, direction)
		if !ok {
			continue
		}
		ownerPID := bestPID
		if owner, found := owners[socket.inode]; found && owner.pid > 0 {
			ownerPID = owner.pid
		}
		if score > bestScore || (score == bestScore && ownerPID < bestPID) {
			bestScore = score
			bestPID = ownerPID
			best = socket
			bestOutbound = outbound
		}
	}
	return best, bestOutbound, bestScore >= 0
}

func matchUDPSocket(packet udpPacket, socket udpSocket, direction int) (bool, int, bool) {
	if packet.family != socket.family || socket.localPort == 0 {
		return false, 0, false
	}
	outScore, outOK := matchUDPEndpoints(socket, packet.source, packet.sourcePort, packet.destination, packet.destinationPort)
	inScore, inOK := matchUDPEndpoints(socket, packet.destination, packet.destinationPort, packet.source, packet.sourcePort)
	if !outOK && !inOK {
		return false, 0, false
	}
	if outOK && (!inOK || outScore > inScore || (outScore == inScore && direction != 0)) {
		return true, outScore, true
	}
	return false, inScore, true
}

func matchUDPEndpoints(socket udpSocket, local netip.Addr, localPort uint16, remote netip.Addr, remotePort uint16) (int, bool) {
	if socket.localPort != localPort || !addressMatches(socket.local, local) {
		return 0, false
	}
	score := 0
	if socket.local.IsValid() && !socket.local.IsUnspecified() {
		score += 2
	}
	if socket.remotePort != 0 {
		if socket.remotePort != remotePort {
			return 0, false
		}
		score++
	}
	if socket.remote.IsValid() && !socket.remote.IsUnspecified() {
		if socket.remote != remote {
			return 0, false
		}
		score += 2
	}
	return score, true
}

func addressMatches(registered, actual netip.Addr) bool {
	return !registered.IsValid() || registered.IsUnspecified() || registered == actual
}

func parseUDPPacket(data []byte) (udpPacket, bool) {
	data = stripPacketLinkHeader(data)
	if len(data) < 1 {
		return udpPacket{}, false
	}
	if len(data) >= 20 && data[0]>>4 == 4 {
		return parseIPv4UDPPacket(data)
	}
	if len(data) >= 40 && data[0]>>4 == 6 {
		return parseIPv6UDPPacket(data)
	}
	return udpPacket{}, false
}

func stripPacketLinkHeader(data []byte) []byte {
	if looksLikeIPv4(data) || looksLikeIPv6(data) {
		return data
	}
	if len(data) >= 14 {
		etherType := binary.BigEndian.Uint16(data[12:14])
		offset := 14
		for isVLAN(etherType) {
			if len(data) < offset+4 {
				return nil
			}
			etherType = binary.BigEndian.Uint16(data[offset+2 : offset+4])
			offset += 4
		}
		if etherType == etherTypeIPv4 || etherType == etherTypeIPv6 {
			return data[offset:]
		}
	}
	if len(data) >= 4 {
		family := binary.LittleEndian.Uint32(data[:4])
		if family == uint32(unix.AF_INET) || family == uint32(unix.AF_INET6) {
			return data[4:]
		}
	}
	if len(data) >= 16 {
		etherType := binary.BigEndian.Uint16(data[14:16])
		offset := 16
		for isVLAN(etherType) {
			if len(data) < offset+4 {
				return nil
			}
			etherType = binary.BigEndian.Uint16(data[offset+2 : offset+4])
			offset += 4
		}
		if etherType == etherTypeIPv4 || etherType == etherTypeIPv6 {
			return data[offset:]
		}
	}
	return data
}

func looksLikeIPv4(data []byte) bool {
	if len(data) < 20 || data[0]>>4 != 4 {
		return false
	}
	headerLength := int(data[0]&0x0f) * 4
	return headerLength >= 20 && headerLength <= len(data)
}

func looksLikeIPv6(data []byte) bool {
	return len(data) >= 40 && data[0]>>4 == 6
}

func isVLAN(etherType uint16) bool {
	return etherType == etherTypeVLAN || etherType == etherTypeVLAN8021AD || etherType == etherTypeVLAN9100
}

func parseIPv4UDPPacket(data []byte) (udpPacket, bool) {
	headerLength := int(data[0]&0x0f) * 4
	if headerLength < 20 || len(data) < headerLength || data[9] != udpProtocol {
		return udpPacket{}, false
	}
	totalLength := int(binary.BigEndian.Uint16(data[2:4]))
	if totalLength < headerLength+8 || totalLength > len(data) {
		return udpPacket{}, false
	}
	return parseUDPHeader(data[headerLength:totalLength], unix.AF_INET,
		netip.AddrFrom4([4]byte{data[12], data[13], data[14], data[15]}),
		netip.AddrFrom4([4]byte{data[16], data[17], data[18], data[19]}))
}

func parseIPv6UDPPacket(data []byte) (udpPacket, bool) {
	payloadLength := int(binary.BigEndian.Uint16(data[4:6]))
	end := 40 + payloadLength
	if payloadLength == 0 || end > len(data) {
		return udpPacket{}, false
	}
	var sourceBytes, destinationBytes [16]byte
	copy(sourceBytes[:], data[8:24])
	copy(destinationBytes[:], data[24:40])
	nextHeader := data[6]
	offset := 40
	for hops := 0; hops < 8; hops++ {
		switch nextHeader {
		case udpProtocol:
			return parseUDPHeader(data[offset:end], unix.AF_INET6, netip.AddrFrom16(sourceBytes), netip.AddrFrom16(destinationBytes))
		case 0, 43, 60, 135, 139, 140:
			if offset+2 > end {
				return udpPacket{}, false
			}
			length := (int(data[offset+1]) + 1) * 8
			if offset+length > end {
				return udpPacket{}, false
			}
			nextHeader = data[offset]
			offset += length
		case 44:
			if offset+8 > end || binary.BigEndian.Uint16(data[offset+2:offset+4])&0xfff8 != 0 {
				return udpPacket{}, false
			}
			nextHeader = data[offset]
			offset += 8
		case 51:
			if offset+2 > end {
				return udpPacket{}, false
			}
			length := (int(data[offset+1]) + 2) * 4
			if offset+length > end {
				return udpPacket{}, false
			}
			nextHeader = data[offset]
			offset += length
		default:
			return udpPacket{}, false
		}
	}
	return udpPacket{}, false
}

func parseUDPHeader(data []byte, family uint8, source, destination netip.Addr) (udpPacket, bool) {
	if len(data) < 8 {
		return udpPacket{}, false
	}
	length := int(binary.BigEndian.Uint16(data[4:6]))
	if length < 8 || length > len(data) {
		return udpPacket{}, false
	}
	return udpPacket{
		family: family, source: source, destination: destination,
		sourcePort:      binary.BigEndian.Uint16(data[:2]),
		destinationPort: binary.BigEndian.Uint16(data[2:4]),
		payload:         uint64(length - 8),
	}, true
}

func htons(value uint16) uint16 {
	return value<<8 | value>>8
}
