package procstats

import (
	"context"
	"encoding/binary"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestJoinWarningsClassifiesAndDeduplicates(t *testing.T) {
	warnings := []collectorWarning{
		{code: warningOwnerRefresh, detail: "permission denied"},
		{code: warningOwnerRefresh, detail: "permission denied"},
		{code: warningUDPFallback, detail: "using packet capture"},
	}
	if got, want := joinWarnings(warnings), "process owner refresh unavailable: permission denied; using packet capture"; got != want {
		t.Fatalf("joinWarnings = %q, want %q", got, want)
	}
}

func TestTCPFallbackWarningRendersAffectedFamilies(t *testing.T) {
	warning := collectorWarning{code: warningTCPFallback, detail: "IPv6: permission denied"}
	want := "TCP byte counters unavailable (IPv6: permission denied); showing connection counts for affected families only"
	if got := warning.message(); got != want {
		t.Fatalf("TCP warning = %q, want %q", got, want)
	}
}

func TestInterfaceResolverResolutionPrecedence(t *testing.T) {
	resolver := interfaceResolver{
		byIP: map[netip.Addr]string{
			netip.MustParseAddr("192.0.2.10"): "address0",
			netip.MustParseAddr("192.0.2.11"): "address1",
		},
		byIndex: map[int]string{7: "index7"},
	}
	tests := []struct {
		name    string
		ifIndex int
		local   string
		remote  string
		want    string
	}{
		{name: "index wins", ifIndex: 7, local: "192.0.2.10", remote: "192.0.2.11", want: "index7"},
		{name: "local address", local: "192.0.2.10", remote: "198.51.100.10", want: "address0"},
		{name: "remote address", local: "198.51.100.10", remote: "192.0.2.11", want: "address1"},
		{name: "loopback fallback", local: "127.0.0.1", remote: "198.51.100.10", want: "lo"},
		{name: "unresolved", local: "198.51.100.10", remote: "203.0.113.10", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := resolver.resolveValues(test.ifIndex, netip.MustParseAddr(test.local), netip.MustParseAddr(test.remote))
			if got != test.want {
				t.Fatalf("resolveValues() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestClassifyUnownedSocketLabelsVPNTransport(t *testing.T) {
	resolver := interfaceResolver{tunnelNames: map[string]struct{}{"proton0": {}}}
	owner := classifyUnownedSocket(0, true, netip.IPv4Unspecified(), netip.IPv4Unspecified(), 55270, resolver)
	if owner.name != "VPN tunnel" || owner.pid != 0 {
		t.Fatalf("VPN owner = %+v", owner)
	}
}

func TestClassifyUnownedSocketLabelsSystemUID(t *testing.T) {
	resolver := interfaceResolver{}
	owner := classifyUnownedSocket(971, true, netip.IPv4Unspecified(), netip.IPv4Unspecified(), 5353, resolver)
	if owner.name != "system (uid 971)" {
		t.Fatalf("system owner = %+v", owner)
	}
}

func TestClassifyUnownedSocketKeepsUnknownWhenUIDUnavailable(t *testing.T) {
	resolver := interfaceResolver{tunnelNames: map[string]struct{}{"proton0": {}}}
	owner := classifyUnownedSocket(0, false, netip.IPv4Unspecified(), netip.IPv4Unspecified(), 55270, resolver)
	if owner.name != "unknown" {
		t.Fatalf("unknown owner = %+v", owner)
	}
}

func TestReadProcessOwnersFixture(t *testing.T) {
	root := t.TempDir()
	processDir := filepath.Join(root, "123")
	if err := os.MkdirAll(filepath.Join(processDir, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(processDir, "comm"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	statFields := append([]string{"S"}, make([]string, 18)...)
	for index := 1; index < len(statFields); index++ {
		statFields[index] = "0"
	}
	statFields = append(statFields, "98765")
	if err := os.WriteFile(filepath.Join(processDir, "stat"), []byte("123 (fixture process) "+strings.Join(statFields, " ")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[4242]", filepath.Join(processDir, "fd", "3")); err != nil {
		t.Fatal(err)
	}

	owners, err := readProcessOwners(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	owner, ok := owners[4242]
	if !ok {
		t.Fatalf("socket owner missing: %+v", owners)
	}
	if owner.pid != 123 || owner.startTime != 98765 || owner.name != "fixture" {
		t.Fatalf("owner = %+v", owner)
	}
}

func TestParseStartTimeRejectsMalformedStat(t *testing.T) {
	for _, stat := range []string{"", "123 process S", "123 (process) S 1", "123 (process) S " + strings.Repeat("0 ", 19) + "not-a-number"} {
		if got := parseStartTime(stat); got != 0 {
			t.Fatalf("parseStartTime(%q) = %d, want zero", stat, got)
		}
	}
}

func TestParseIPv6UDPPacket(t *testing.T) {
	source := netip.MustParseAddr("2001:db8::1")
	destination := netip.MustParseAddr("2001:db8::2")
	data := make([]byte, 40+8+5)
	data[0] = 0x60
	data[6] = udpProtocol
	binary.BigEndian.PutUint16(data[4:6], uint16(len(data)-40))
	sourceBytes := source.As16()
	destinationBytes := destination.As16()
	copy(data[8:24], sourceBytes[:])
	copy(data[24:40], destinationBytes[:])
	udp := data[40:]
	binary.BigEndian.PutUint16(udp[0:2], 5353)
	binary.BigEndian.PutUint16(udp[2:4], 5354)
	binary.BigEndian.PutUint16(udp[4:6], uint16(len(udp)))

	packet, ok := parseIPv6UDPPacket(data)
	if !ok {
		t.Fatal("parseIPv6UDPPacket returned false")
	}
	if packet.family != unix.AF_INET6 || packet.source != source || packet.destination != destination || packet.sourcePort != 5353 || packet.destinationPort != 5354 || packet.payload != 5 {
		t.Fatalf("packet = %+v", packet)
	}
}

func TestParseIPv6UDPPacketWithHopByHopHeader(t *testing.T) {
	data := make([]byte, 40+8+8+1)
	data[0] = 0x60
	data[6] = 0 // Hop-by-Hop Options
	binary.BigEndian.PutUint16(data[4:6], uint16(len(data)-40))
	data[40] = udpProtocol
	data[41] = 0 // eight-byte extension header
	udp := data[48:]
	binary.BigEndian.PutUint16(udp[0:2], 1000)
	binary.BigEndian.PutUint16(udp[2:4], 1001)
	binary.BigEndian.PutUint16(udp[4:6], uint16(len(udp)))

	packet, ok := parseIPv6UDPPacket(data)
	if !ok || packet.sourcePort != 1000 || packet.destinationPort != 1001 || packet.payload != 1 {
		t.Fatalf("packet = %+v, ok=%v", packet, ok)
	}
}

func TestParseNetlinkMessagesRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{name: "short length", data: netlinkHeader(3, unix.NLMSG_DONE), want: trueError},
		{name: "truncated message", data: netlinkHeader(uint32(nlmsgHeaderSize+4), unix.NLMSG_DONE), want: trueError},
		{name: "short error payload", data: netlinkErrorMessage([]byte{1, 2}), want: trueError},
		{name: "kernel error", data: netlinkErrorMessage(binary.NativeEndian.AppendUint32(nil, ^uint32(unix.EINVAL)+1)), want: syscall.EINVAL},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := parseNetlinkMessages(test.data, unix.AF_INET)
			if test.want == trueError {
				if err == nil {
					t.Fatal("malformed message was accepted")
				}
				return
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseNetlinkMessagesEmptyAndDone(t *testing.T) {
	message := netlinkHeader(nlmsgHeaderSize, unix.NLMSG_DONE)
	sockets, done, err := parseNetlinkMessages(message, unix.AF_INET)
	if err != nil || !done || len(sockets) != 0 {
		t.Fatalf("result = sockets:%d done:%v err:%v", len(sockets), done, err)
	}
}

func TestSaturatingAdd(t *testing.T) {
	if got := saturatingAdd(^uint64(0)-1, 2); got != ^uint64(0) {
		t.Fatalf("saturatingAdd overflow = %d, want max uint64", got)
	}
	if got := saturatingAdd(4, 5); got != 9 {
		t.Fatalf("saturatingAdd regular = %d, want 9", got)
	}
}

var trueError = errors.New("expected an error")

func netlinkHeader(length uint32, typeID uint16) []byte {
	message := make([]byte, nlmsgHeaderSize)
	binary.NativeEndian.PutUint32(message[0:4], length)
	binary.NativeEndian.PutUint16(message[4:6], typeID)
	return message
}

func netlinkErrorMessage(payload []byte) []byte {
	message := netlinkHeader(uint32(nlmsgHeaderSize+len(payload)), unix.NLMSG_ERROR)
	return append(message, payload...)
}
