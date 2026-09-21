package procstats

import "net/netip"

type socketKey struct {
	cookie [2]uint32
	inode  uint64
}

type diagSocket struct {
	inode   uint64
	ifIndex int
	cookie  [2]uint32
	local   netip.Addr
	remote  netip.Addr
	rxBytes uint64
	txBytes uint64
}

// udpSocket is a row from /proc/net/udp{,6}. UDP has no kernel byte counter
// comparable to TCP_INFO, so the socket identity is used to attribute packets
// observed by the optional AF_PACKET capture.
type udpSocket struct {
	inode      uint64
	family     uint8
	local      netip.Addr
	remote     netip.Addr
	localPort  uint16
	remotePort uint16
}

type processKey struct {
	pid           int
	startTime     uint64
	interfaceName string
}

type udpTraffic struct {
	inode uint64
	rx    uint64
	tx    uint64
}

func saturatingAdd(left, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}
