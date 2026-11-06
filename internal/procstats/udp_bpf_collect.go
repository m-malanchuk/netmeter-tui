package procstats

import (
	"context"
	"fmt"

	"golang.org/x/sys/unix"
)

const udpBPFStaleSamples = 3

type udpBPFIdentity struct {
	inode  uint64
	owner  processOwner
	socket udpSocket
}

func diagSocketCookie(socket diagSocket) uint64 {
	if socket.cookie == [2]uint32{inetDiagNoCookie, inetDiagNoCookie} {
		return 0
	}
	return uint64(socket.cookie[0]) | uint64(socket.cookie[1])<<32
}

func (c *Collector) collectBPFUDP(ctx context.Context, sockets []udpSocket, owners map[uint64]processOwner, resolver interfaceResolver) (map[processKey]udpTraffic, collectorWarning, error) {
	byInode := make(map[uint64]udpSocket, len(sockets))
	for _, socket := range sockets {
		byInode[socket.inode] = socket
	}
	identities := make(map[uint64]udpBPFIdentity)
	for _, family := range []uint8{unix.AF_INET, unix.AF_INET6} {
		diagContext, cancel := context.WithTimeout(ctx, tcpDiagTimeout)
		observed, err := dumpSocketFamily(diagContext, family, udpProtocol)
		cancel()
		if err != nil {
			return nil, collectorWarning{}, fmt.Errorf("query UDP socket identities: %w", err)
		}
		for _, diag := range observed {
			cookie := diagSocketCookie(diag)
			if cookie == 0 {
				continue
			}
			socket, found := byInode[diag.inode]
			if !found {
				socket = udpSocket{
					inode: diag.inode, family: family,
					local: diag.local, remote: diag.remote,
					localPort: diag.localPort, remotePort: diag.remotePort,
					uid: diag.uid, uidKnown: diag.uidKnown,
				}
			}
			owner, found := owners[diag.inode]
			if !found {
				owner = classifyUnownedSocket(socket.uid, socket.uidKnown, socket.local, socket.remote, socket.localPort, resolver)
			}
			identities[cookie] = udpBPFIdentity{inode: diag.inode, owner: owner, socket: socket}
		}
	}
	values, failures, err := c.udpBPF.snapshot()
	if err != nil {
		return nil, collectorWarning{}, err
	}
	warning := collectorWarning{}
	if failures != 0 {
		warning = collectorWarning{code: warningUDPAccounting, detail: "UDP eBPF counter capacity exceeded; some byte counts may be incomplete"}
	}
	return c.applyBPFUDPSnapshot(values, identities, resolver), warning, nil
}

func (c *Collector) applyBPFUDPSnapshot(values map[udpBPFKey]udpBPFValue, identities map[uint64]udpBPFIdentity, resolver interfaceResolver) map[processKey]udpTraffic {
	traffic := make(map[processKey]udpTraffic)
	for cookie, identity := range identities {
		c.udpOwners[cookie] = identity
	}
	for key, value := range values {
		identity, found := identities[key.Cookie]
		if !found {
			identity, found = c.udpOwners[key.Cookie]
		}
		if !found {
			// Keep the absolute counter untouched until the socket can be
			// resolved by inet_diag in a later sample.
			continue
		}
		previous, seen := c.udpPrevious[key]
		c.udpPrevious[key] = value
		rx, tx := value.RX, value.TX
		if seen {
			rx, tx = 0, 0
			if value.RX >= previous.RX {
				rx = value.RX - previous.RX
			}
			if value.TX >= previous.TX {
				tx = value.TX - previous.TX
			}
		}
		interfaceName := resolver.byIndex[int(key.IfIndex)]
		if interfaceName == "" {
			interfaceName = resolver.resolveUDP(identity.socket)
		}
		if identity.owner.pid == 0 {
			identity.owner = classifyUnownedSocket(identity.socket.uid, identity.socket.uidKnown, identity.socket.local, identity.socket.remote, identity.socket.localPort, resolver)
		}
		process := processKey{pid: identity.owner.pid, startTime: identity.owner.startTime, interfaceName: interfaceName}
		observed := traffic[process]
		observed.inode = identity.inode
		observed.rx = saturatingAdd(observed.rx, rx)
		observed.tx = saturatingAdd(observed.tx, tx)
		traffic[process] = observed
	}
	c.pruneBPFState(values, identities)
	return traffic
}

func (c *Collector) pruneBPFState(values map[udpBPFKey]udpBPFValue, identities map[uint64]udpBPFIdentity) {
	retained := make(map[udpBPFKey]struct{}, len(values))
	for key := range values {
		if _, found := identities[key.Cookie]; found {
			delete(c.udpBPFStale, key)
			retained[key] = struct{}{}
			continue
		}
		c.udpBPFStale[key]++
		if c.udpBPFStale[key] < udpBPFStaleSamples {
			retained[key] = struct{}{}
			continue
		}
		delete(c.udpBPFStale, key)
		delete(c.udpPrevious, key)
		if c.udpBPF != nil {
			_ = c.udpBPF.delete(key)
		}
	}

	for key := range c.udpPrevious {
		if _, found := values[key]; !found {
			delete(c.udpPrevious, key)
			delete(c.udpBPFStale, key)
		}
	}

	referenced := make(map[uint64]struct{}, len(retained)+len(identities))
	for key := range retained {
		referenced[key.Cookie] = struct{}{}
	}
	for cookie := range identities {
		referenced[cookie] = struct{}{}
	}
	for cookie := range c.udpOwners {
		if _, found := referenced[cookie]; !found {
			delete(c.udpOwners, cookie)
		}
	}
}
