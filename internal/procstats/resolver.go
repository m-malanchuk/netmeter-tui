package procstats

import (
	"net"
	"net/netip"
)

type interfaceResolver struct {
	byIP    map[netip.Addr]string
	byIndex map[int]string
}

func newInterfaceResolver() interfaceResolver {
	resolver := interfaceResolver{byIP: make(map[netip.Addr]string), byIndex: make(map[int]string)}
	interfaces, err := net.Interfaces()
	if err != nil {
		return resolver
	}
	for _, networkInterface := range interfaces {
		resolver.byIndex[networkInterface.Index] = networkInterface.Name
		addresses, err := networkInterface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err == nil {
				resolver.byIP[prefix.Addr()] = networkInterface.Name
				continue
			}
			if parsed, err := netip.ParseAddr(address.String()); err == nil {
				resolver.byIP[parsed] = networkInterface.Name
			}
		}
	}
	return resolver
}

func (resolver interfaceResolver) resolve(socket diagSocket) string {
	return resolver.resolveValues(socket.ifIndex, socket.local, socket.remote)
}

func (resolver interfaceResolver) resolveUDP(socket udpSocket) string {
	return resolver.resolveValues(0, socket.local, socket.remote)
}

func (resolver interfaceResolver) resolveValues(ifIndex int, local, remote netip.Addr) string {
	if ifIndex != 0 {
		if name := resolver.byIndex[ifIndex]; name != "" {
			return name
		}
	}
	if name := resolver.byIP[local]; name != "" {
		return name
	}
	if name := resolver.byIP[remote]; name != "" {
		return name
	}
	if local.IsLoopback() || remote.IsLoopback() {
		return "lo"
	}
	return ""
}
