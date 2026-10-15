package procstats

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"testing"
)

func TestUDPBPFCookieFromDiag(t *testing.T) {
	tests := []struct {
		name   string
		cookie [2]uint32
		want   uint64
	}{
		{name: "valid", cookie: [2]uint32{0x12345678, 0x9abcdef0}, want: 0x9abcdef012345678},
		{name: "unavailable", cookie: [2]uint32{inetDiagNoCookie, inetDiagNoCookie}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := diagSocketCookie(diagSocket{cookie: test.cookie}); got != test.want {
				t.Fatalf("cookie = %#x, want %#x", got, test.want)
			}
		})
	}
}

func TestApplyBPFUDPSnapshot(t *testing.T) {
	collector := NewCollector()
	key := udpBPFKey{Cookie: 42, IfIndex: 7}
	owner := processOwner{pid: 123, startTime: 9, name: "browser"}
	identity := udpBPFIdentity{
		inode: 88, owner: owner,
		socket: udpSocket{inode: 88, local: netip.MustParseAddr("192.0.2.10")},
	}
	resolver := interfaceResolver{byIP: make(map[netip.Addr]string), byIndex: map[int]string{7: "eth0"}}
	process := processKey{pid: 123, startTime: 9, interfaceName: "eth0"}
	first := collector.applyBPFUDPSnapshot(
		map[udpBPFKey]udpBPFValue{key: {RX: 1000, TX: 200}},
		map[uint64]udpBPFIdentity{42: identity}, resolver,
	)
	if got := first[process]; got.rx != 1000 || got.tx != 200 {
		t.Fatalf("first UDP delta = %+v", got)
	}
	second := collector.applyBPFUDPSnapshot(
		map[udpBPFKey]udpBPFValue{key: {RX: 2500, TX: 450}},
		nil, resolver,
	)
	if got := second[process]; got.rx != 1500 || got.tx != 250 {
		t.Fatalf("closed-socket UDP delta = %+v", got)
	}
	reset := collector.applyBPFUDPSnapshot(
		map[udpBPFKey]udpBPFValue{key: {RX: 100, TX: 20}},
		nil, resolver,
	)
	if got := reset[process]; got.rx != 0 || got.tx != 0 {
		t.Fatalf("counter reset delta = %+v", got)
	}
}

func TestApplyBPFUDPWaitsForIdentity(t *testing.T) {
	collector := NewCollector()
	key := udpBPFKey{Cookie: 42, IfIndex: 3}
	values := map[udpBPFKey]udpBPFValue{key: {RX: 500}}
	resolver := interfaceResolver{byIP: make(map[netip.Addr]string), byIndex: map[int]string{3: "eth0"}}
	if got := collector.applyBPFUDPSnapshot(values, nil, resolver); len(got) != 0 {
		t.Fatalf("unresolved bytes were attributed: %+v", got)
	}
	owner := processOwner{pid: 22, name: "player"}
	got := collector.applyBPFUDPSnapshot(values, map[uint64]udpBPFIdentity{
		42: {inode: 4, owner: owner},
	}, resolver)
	if observed := got[processKey{pid: 22, interfaceName: "eth0"}]; observed.rx != 500 {
		t.Fatalf("resolved buffered bytes = %+v", observed)
	}
}

func TestUDPBPFInstructionsMarshal(t *testing.T) {
	for _, outbound := range []bool{false, true} {
		var encoded bytes.Buffer
		if err := udpBPFInstructions(1, 2, outbound).Marshal(&encoded, binary.LittleEndian); err != nil {
			t.Fatalf("marshal outbound=%v: %v", outbound, err)
		}
		if encoded.Len() == 0 {
			t.Fatalf("empty program outbound=%v", outbound)
		}
	}
}

func TestPruneBPFStateRemovesStaleSocketState(t *testing.T) {
	collector := NewCollector()
	key := udpBPFKey{Cookie: 9, IfIndex: 2}
	collector.udpPrevious[key] = udpBPFValue{RX: 10}
	collector.udpOwners[key.Cookie] = udpBPFIdentity{inode: 11}
	values := map[udpBPFKey]udpBPFValue{key: {RX: 10}}

	for sample := 0; sample < udpBPFStaleSamples-1; sample++ {
		collector.pruneBPFState(values, nil)
		if _, found := collector.udpPrevious[key]; !found {
			t.Fatalf("socket state removed before stale grace period at sample %d", sample)
		}
	}
	collector.pruneBPFState(values, nil)
	if _, found := collector.udpPrevious[key]; found {
		t.Fatal("stale socket previous value was retained")
	}
	if _, found := collector.udpBPFStale[key]; found {
		t.Fatal("stale marker was retained")
	}
	if _, found := collector.udpOwners[key.Cookie]; found {
		t.Fatal("stale socket owner was retained")
	}
}
