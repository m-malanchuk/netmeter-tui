package procstats

import (
	"fmt"
	"net/netip"
)

const (
	unknownOwnerName = "unknown"
	vpnOwnerName     = "VPN tunnel"
)

func classifyUnownedSocket(uid uint32, uidKnown bool, local, remote netip.Addr, localPort uint16, resolver interfaceResolver) processOwner {
	if !uidKnown {
		return processOwner{name: unknownOwnerName}
	}
	// A VPN tunnel's kernel/transport socket can have no visible /proc/<pid>/fd
	// owner even though it carries application traffic. Keep it separate from
	// normal system sockets rather than attributing its bytes to an application.
	if resolver.hasTunnel() && uid == 0 && localPort >= 1024 &&
		(!local.IsValid() || local.IsUnspecified()) &&
		(!remote.IsValid() || remote.IsUnspecified()) {
		return processOwner{name: vpnOwnerName}
	}
	return processOwner{name: fmt.Sprintf("system (uid %d)", uid)}
}
