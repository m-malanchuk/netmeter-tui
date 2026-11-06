package procstats

import (
	"context"
	"encoding/binary"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestParseProcessHelpers(t *testing.T) {
	if got, ok := parseSocketInode("socket:[12345]"); !ok || got != 12345 {
		t.Fatalf("parseSocketInode = %d/%v", got, ok)
	}
	if _, ok := parseSocketInode("pipe:[12345]"); ok {
		t.Fatal("pipe was parsed as a socket")
	}
	stat := "1234 (worker with spaces) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 98765 21"
	if got := parseStartTime(stat); got != 98765 {
		t.Fatalf("parseStartTime = %d, want 98765", got)
	}
}

func TestMergeProcessOwnersPrefersResolvedLowestPID(t *testing.T) {
	destination := map[uint64]processOwner{
		1: {name: "unknown"},
		2: {pid: 50, name: "later"},
	}
	mergeProcessOwners(destination, map[uint64]processOwner{
		1: {pid: 20, name: "browser"},
		2: {pid: 40, name: "earlier"},
	})
	if destination[1].pid != 20 || destination[1].name != "browser" {
		t.Fatalf("resolved owner = %+v", destination[1])
	}
	if destination[2].pid != 40 || destination[2].name != "earlier" {
		t.Fatalf("shared owner = %+v", destination[2])
	}
}

func TestOwnersNeedRefresh(t *testing.T) {
	owners := map[uint64]processOwner{10: {pid: 1, name: "known"}}
	if ownersNeedRefresh(owners, []diagSocket{{inode: 10}}, []udpSocket{{inode: 10}}) {
		t.Fatal("fully resolved socket set requested another procfs scan")
	}
	if !ownersNeedRefresh(owners, []diagSocket{{inode: 20}}, nil) {
		t.Fatal("unresolved TCP socket did not request another procfs scan")
	}
	if ownersNeedRefresh(owners, []diagSocket{{inode: 0}}, nil) {
		t.Fatal("ownerless kernel socket requested another procfs scan")
	}
}

func TestParseProcSocketLine(t *testing.T) {
	line := "  7: 0100007F:C350 0200000A:0016 01 00000000:00000000 02:00000100 00000000   1000        0 4242 1 0000000000000000 20 4 30 10 -1"
	socket, ok := parseProcSocketLine(line, unix.AF_INET)
	if !ok {
		t.Fatal("parseProcSocketLine returned false")
	}
	if socket.inode != 4242 || socket.local != netip.MustParseAddr("127.0.0.1") || socket.remote != netip.MustParseAddr("10.0.0.2") {
		t.Fatalf("socket = %+v", socket)
	}
}

func TestParseProcUDPSocketLine(t *testing.T) {
	line := "  3: 0100007F:1F90 00000000:0000 07 00000000:00000000 00:00000000 00000000 1000 0 7777 2"
	socket, ok := parseProcUDPSocketLine(line, unix.AF_INET)
	if !ok {
		t.Fatal("parseProcUDPSocketLine returned false")
	}
	if socket.inode != 7777 || socket.local != netip.MustParseAddr("127.0.0.1") || socket.localPort != 8080 {
		t.Fatalf("socket = %+v", socket)
	}
	if socket.remote != netip.MustParseAddr("0.0.0.0") || socket.remotePort != 0 {
		t.Fatalf("remote endpoint = %s:%d", socket.remote, socket.remotePort)
	}
	if socket.uid != 1000 || !socket.uidKnown {
		t.Fatalf("socket uid = %d/%v, want 1000/true", socket.uid, socket.uidKnown)
	}
}

func TestReadProcUDPSockets(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "   sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops\n" +
		"  288: 6801A8C0:9FA4 00000000:0000 07 00000000:00000000 00:00000000 00000000 1000 0 310783 2 000000000305b908 0\n"
	if err := os.WriteFile(filepath.Join(root, "net", "udp"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	sockets, err := readProcUDPSockets(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sockets) != 1 || sockets[0].inode != 310783 || sockets[0].local != netip.MustParseAddr("192.168.1.104") || sockets[0].localPort != 40868 {
		t.Fatalf("sockets = %+v", sockets)
	}
}

func TestParseUDPPacketIPv4(t *testing.T) {
	data := make([]byte, 14+20+8+4)
	binary.BigEndian.PutUint16(data[12:14], etherTypeIPv4)
	ip := data[14:]
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], uint16(len(ip)))
	ip[9] = udpProtocol
	ip[12], ip[13], ip[14], ip[15] = 10, 0, 0, 1
	ip[16], ip[17], ip[18], ip[19] = 10, 0, 0, 2
	udp := ip[20:]
	binary.BigEndian.PutUint16(udp[0:2], 40000)
	binary.BigEndian.PutUint16(udp[2:4], 443)
	binary.BigEndian.PutUint16(udp[4:6], 12)
	packet, ok := parseUDPPacket(data)
	if !ok {
		t.Fatal("parseUDPPacket returned false")
	}
	if packet.family != unix.AF_INET || packet.source != netip.MustParseAddr("10.0.0.1") || packet.destinationPort != 443 || packet.payload != 4 {
		t.Fatalf("packet = %+v", packet)
	}
}

func TestParseUDPPacketRejectsIPv4Fragments(t *testing.T) {
	data := make([]byte, 14+20+8+4)
	binary.BigEndian.PutUint16(data[12:14], etherTypeIPv4)
	ip := data[14:]
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], uint16(len(ip)))
	binary.BigEndian.PutUint16(ip[6:8], 0x2000) // more-fragments flag
	ip[9] = udpProtocol
	if _, ok := parseUDPPacket(data); ok {
		t.Fatal("fragmented IPv4 datagram was accepted")
	}
}

func TestParseUDPPacketRejectsIPv6MoreFragments(t *testing.T) {
	data := make([]byte, 14+40+8+8+4)
	binary.BigEndian.PutUint16(data[12:14], etherTypeIPv6)
	ip := data[14:]
	ip[0] = 0x60
	binary.BigEndian.PutUint16(ip[4:6], uint16(len(ip)-40))
	ip[6] = 44 // fragment extension header
	fragment := ip[40:]
	fragment[0] = udpProtocol
	binary.BigEndian.PutUint16(fragment[2:4], 1) // more-fragments flag
	udp := fragment[8:]
	binary.BigEndian.PutUint16(udp[4:6], 12)
	if _, ok := parseUDPPacket(data); ok {
		t.Fatal("fragmented IPv6 datagram was accepted")
	}
}

func TestMatchUDPSocketDirection(t *testing.T) {
	socket := udpSocket{family: unix.AF_INET, local: netip.IPv4Unspecified(), localPort: 53}
	packet := udpPacket{
		family: unix.AF_INET, source: netip.MustParseAddr("10.0.0.2"), sourcePort: 40000,
		destination: netip.MustParseAddr("10.0.0.1"), destinationPort: 53,
	}
	if outbound, _, ok := matchUDPSocket(packet, socket, 0); !ok || outbound {
		t.Fatalf("incoming packet matched as outbound: %v/%v", outbound, ok)
	}
	outgoing := udpPacket{
		family: unix.AF_INET, source: netip.MustParseAddr("10.0.0.1"), sourcePort: 53,
		destination: netip.MustParseAddr("10.0.0.2"), destinationPort: 40000,
	}
	if outbound, _, ok := matchUDPSocket(outgoing, socket, 1); !ok || !outbound {
		t.Fatalf("outgoing hint did not select outbound direction: %v/%v", outbound, ok)
	}
}

func TestMatchUDPSocketPrefersConnectedEndpoint(t *testing.T) {
	packet := udpPacket{
		family: unix.AF_INET, source: netip.MustParseAddr("10.0.0.2"), sourcePort: 40000,
		destination: netip.MustParseAddr("10.0.0.1"), destinationPort: 53,
	}
	sockets := []udpSocket{
		{inode: 1, family: unix.AF_INET, local: netip.IPv4Unspecified(), localPort: 53},
		{inode: 2, family: unix.AF_INET, local: netip.MustParseAddr("10.0.0.1"), localPort: 53,
			remote: netip.MustParseAddr("10.0.0.2"), remotePort: 40000},
	}
	matched, outbound, ok := matchUDPPacket(packet, sockets, map[uint64]processOwner{1: {pid: 10}, 2: {pid: 20}}, 0)
	if !ok || outbound || matched.inode != 2 {
		t.Fatalf("connected UDP socket was not preferred: socket=%+v outbound=%v ok=%v", matched, outbound, ok)
	}
}

func TestMatchUDPSocketSharedEndpointUsesDeterministicOwner(t *testing.T) {
	packet := udpPacket{
		family: unix.AF_INET, source: netip.MustParseAddr("192.0.2.10"), sourcePort: 53000,
		destination: netip.MustParseAddr("192.0.2.53"), destinationPort: 53,
	}
	sockets := []udpSocket{
		{inode: 20, family: unix.AF_INET, local: netip.IPv4Unspecified(), localPort: 53},
		{inode: 10, family: unix.AF_INET, local: netip.IPv4Unspecified(), localPort: 53},
	}
	owners := map[uint64]processOwner{20: {pid: 200}, 10: {pid: 100}}
	matched, _, ok := matchUDPPacket(packet, sockets, owners, 0)
	if !ok || matched.inode != 10 {
		t.Fatalf("shared UDP endpoint match = %+v/%v, want lowest owner PID", matched, ok)
	}
}

func TestParseDiagSocketTCPInfo(t *testing.T) {
	payload := make([]byte, inetDiagMsgSize+4+208)
	payload[0] = unix.AF_INET
	payload[1] = 1
	binary.NativeEndian.PutUint32(payload[40:44], 7)
	binary.NativeEndian.PutUint32(payload[44:48], 11)
	binary.NativeEndian.PutUint32(payload[48:52], 12)
	binary.NativeEndian.PutUint32(payload[64:68], 1000)
	binary.NativeEndian.PutUint32(payload[68:72], 99)
	payload[8], payload[9], payload[10], payload[11] = 127, 0, 0, 1
	payload[24], payload[25], payload[26], payload[27] = 8, 8, 8, 8
	binary.NativeEndian.PutUint16(payload[72:74], uint16(4+208))
	binary.NativeEndian.PutUint16(payload[74:76], inetDiagInfoAttr)
	binary.NativeEndian.PutUint64(payload[4+inetDiagMsgSize+128:4+inetDiagMsgSize+136], 1234)
	binary.NativeEndian.PutUint64(payload[4+inetDiagMsgSize+200:4+inetDiagMsgSize+208], 5678)
	socket, ok := parseDiagSocket(payload, unix.AF_INET)
	if !ok {
		t.Fatal("parseDiagSocket returned false")
	}
	if socket.inode != 99 || socket.ifIndex != 7 || socket.cookie != [2]uint32{11, 12} {
		t.Fatalf("socket identity = %+v", socket)
	}
	if socket.uid != 1000 || !socket.uidKnown {
		t.Fatalf("socket uid = %d/%v, want 1000/true", socket.uid, socket.uidKnown)
	}
	if socket.local != netip.MustParseAddr("127.0.0.1") || socket.remote != netip.MustParseAddr("8.8.8.8") {
		t.Fatalf("addresses = %s/%s", socket.local, socket.remote)
	}
	if socket.rxBytes != 1234 || socket.txBytes != 5678 {
		t.Fatalf("counters = %d/%d", socket.rxBytes, socket.txBytes)
	}
}

func TestParseDiagSocketRejectsOwnerlessSocket(t *testing.T) {
	payload := make([]byte, inetDiagMsgSize)
	payload[0] = unix.AF_INET
	if _, ok := parseDiagSocket(payload, unix.AF_INET); ok {
		t.Fatal("ownerless socket with inode zero was accepted")
	}
}

func TestMakeDiagRequest(t *testing.T) {
	request := makeDiagRequest(unix.AF_INET6)
	if len(request) != nlmsgHeaderSize+inetDiagReqSize {
		t.Fatalf("request length = %d", len(request))
	}
	if binary.NativeEndian.Uint32(request[0:4]) != uint32(len(request)) {
		t.Fatalf("header length = %d", binary.NativeEndian.Uint32(request[0:4]))
	}
	if binary.NativeEndian.Uint16(request[4:6]) != unix.SOCK_DIAG_BY_FAMILY {
		t.Fatalf("request type = %d", binary.NativeEndian.Uint16(request[4:6]))
	}
	if binary.NativeEndian.Uint16(request[6:8]) != unix.NLM_F_REQUEST|unix.NLM_F_DUMP {
		t.Fatalf("request flags = %#x", binary.NativeEndian.Uint16(request[6:8]))
	}
	if binary.NativeEndian.Uint32(request[8:12]) != 1 {
		t.Fatalf("request sequence = %d", binary.NativeEndian.Uint32(request[8:12]))
	}
	if request[nlmsgHeaderSize] != unix.AF_INET6 || request[nlmsgHeaderSize+1] != tcpProtocol {
		t.Fatalf("request family/protocol = %d/%d", request[nlmsgHeaderSize], request[nlmsgHeaderSize+1])
	}
	if binary.NativeEndian.Uint32(request[nlmsgHeaderSize+48:nlmsgHeaderSize+52]) != inetDiagNoCookie {
		t.Fatal("request did not set the first no-cookie word")
	}
}

func TestMakeUDPDiagRequest(t *testing.T) {
	request := makeDiagRequestForProtocol(unix.AF_INET, udpProtocol)
	payload := request[nlmsgHeaderSize:]
	if payload[0] != unix.AF_INET || payload[1] != udpProtocol {
		t.Fatalf("UDP request family/protocol = %d/%d", payload[0], payload[1])
	}
	if payload[2] != 0 {
		t.Fatalf("UDP request unexpectedly requested TCP_INFO: %#x", payload[2])
	}
}

func TestParseNetlinkDoneHeader(t *testing.T) {
	message := make([]byte, nlmsgHeaderSize)
	binary.NativeEndian.PutUint32(message[0:4], uint32(len(message)))
	binary.NativeEndian.PutUint16(message[4:6], unix.NLMSG_DONE)
	binary.NativeEndian.PutUint32(message[8:12], 7)

	sockets, done, err := parseNetlinkMessages(message, unix.AF_INET)
	if err != nil {
		t.Fatal(err)
	}
	if !done || len(sockets) != 0 {
		t.Fatalf("done/sockets = %v/%d", done, len(sockets))
	}
}

func TestParseNetlinkRejectsInterruptedDump(t *testing.T) {
	message := make([]byte, nlmsgHeaderSize)
	binary.NativeEndian.PutUint32(message[0:4], uint32(len(message)))
	binary.NativeEndian.PutUint16(message[4:6], unix.NLMSG_DONE)
	binary.NativeEndian.PutUint16(message[6:8], unix.NLM_F_DUMP_INTR)

	if _, _, err := parseNetlinkMessages(message, unix.AF_INET); err == nil {
		t.Fatal("interrupted netlink dump was accepted")
	}
}

func TestParseNetlinkRejectsTrailingHeaderBytes(t *testing.T) {
	message := make([]byte, nlmsgHeaderSize+1)
	binary.NativeEndian.PutUint32(message[0:4], uint32(nlmsgHeaderSize))
	binary.NativeEndian.PutUint16(message[4:6], unix.NLMSG_DONE)
	if _, _, err := parseNetlinkMessages(message, unix.AF_INET); err == nil {
		t.Fatal("trailing partial netlink header was accepted")
	}
}

func TestParseNetlinkRejectsUnexpectedSequence(t *testing.T) {
	message := make([]byte, nlmsgHeaderSize)
	binary.NativeEndian.PutUint32(message[0:4], uint32(len(message)))
	binary.NativeEndian.PutUint16(message[4:6], unix.NLMSG_DONE)
	binary.NativeEndian.PutUint32(message[8:12], 9)
	if _, _, err := parseNetlinkMessagesForSequence(message, unix.AF_INET, diagRequestSeq); err == nil {
		t.Fatal("unexpected netlink sequence was accepted")
	}
}

func TestParseNetlinkErrorMessage(t *testing.T) {
	message := make([]byte, nlmsgHeaderSize+4)
	binary.NativeEndian.PutUint32(message[0:4], uint32(len(message)))
	binary.NativeEndian.PutUint16(message[4:6], unix.NLMSG_ERROR)
	binary.NativeEndian.PutUint32(message[nlmsgHeaderSize:nlmsgHeaderSize+4], uint32(^uint32(0)-uint32(unix.EINVAL)+1))
	if _, _, err := parseNetlinkMessages(message, unix.AF_INET); err == nil {
		t.Fatal("kernel netlink error was accepted")
	}
}

func TestParseDiagSocketRejectsMalformedAttribute(t *testing.T) {
	payload := make([]byte, inetDiagMsgSize+4)
	payload[0] = unix.AF_INET
	binary.NativeEndian.PutUint32(payload[68:72], 99)
	binary.NativeEndian.PutUint16(payload[72:74], 2)
	if _, ok := parseDiagSocket(payload, unix.AF_INET); ok {
		t.Fatal("malformed inet_diag attribute was accepted")
	}
}

func TestParseProcSocketIPv6(t *testing.T) {
	line := "  7: 00000000000000000000000001000000:C350 00000000000000000000000002000000:0016 01 00000000:00000000 00:00000000 00 1000 0 4242 1"
	socket, ok := parseProcSocketLine(line, unix.AF_INET6)
	if !ok {
		t.Fatal("parseProcSocketLine returned false for IPv6 fixture")
	}
	if socket.inode != 4242 || socket.local != netip.MustParseAddr("::1") || socket.remote != netip.MustParseAddr("::2") {
		t.Fatalf("IPv6 socket = %+v", socket)
	}
}

func TestParseProcSocketRejectsMalformedRows(t *testing.T) {
	tests := []string{
		"",
		"  1: 0100007F:C350 0200000A:0016 xx 00:00 00:00 00 1000 0 4242",
		"  1: 0100007F:C350 0200000A:0016 01 00:00 00:00 00 1000 0 not-a-number",
		"  1: 0100007F:C350 0200000A:0016 06 00:00 00:00 00 1000 0 4242",
	}
	for _, line := range tests {
		if _, ok := parseProcSocketLine(line, unix.AF_INET); ok {
			t.Fatalf("malformed proc row was accepted: %q", line)
		}
	}
}

func TestReadProcSocketFamilyMissingFile(t *testing.T) {
	sockets, err := readProcSocketFamily(context.Background(), t.TempDir(), unix.AF_INET)
	if err != nil {
		t.Fatal(err)
	}
	if sockets != nil {
		t.Fatalf("missing procfs socket table = %#v, want nil", sockets)
	}
}

func FuzzParseNetlinkMessages(f *testing.F) {
	done := make([]byte, nlmsgHeaderSize)
	binary.NativeEndian.PutUint32(done[0:4], uint32(len(done)))
	binary.NativeEndian.PutUint16(done[4:6], unix.NLMSG_DONE)
	f.Add(done)
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = parseNetlinkMessages(data, unix.AF_INET)
	})
}

func FuzzParseUDPPacket(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x45, 0, 0, 20})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseUDPPacket(data)
	})
}
