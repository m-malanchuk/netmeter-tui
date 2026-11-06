package procstats

import "net/netip"

type socketKey struct {
	// The cookie is the kernel socket generation identity. Inode alone can be
	// reused after a socket closes, so both fields are retained for deltas.
	cookie [2]uint32
	// inode links an inet_diag row to /proc/<pid>/fd socket symlinks.
	inode uint64
}

type diagSocket struct {
	// inode and cookie identify one TCP/UDP kernel socket generation. The
	// endpoint fields are network-byte-order decoded addresses and ports.
	inode      uint64
	ifIndex    int
	cookie     [2]uint32
	local      netip.Addr
	remote     netip.Addr
	localPort  uint16
	remotePort uint16
	// rxBytes and txBytes are cumulative TCP_INFO counters. They are not
	// populated by the procfs fallback, which only supplies connection rows.
	rxBytes uint64
	txBytes uint64
}

// udpSocket is a row from /proc/net/udp{,6}. UDP has no kernel byte counter
// comparable to TCP_INFO, so the socket identity is used to attribute packets
// observed by the optional AF_PACKET capture.
type udpSocket struct {
	// UDP procfs rows have no byte counters. inode identifies the userspace
	// owner, while endpoints are used to match AF_PACKET observations.
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
