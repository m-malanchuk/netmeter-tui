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

func TestParseDiagSocketTCPInfo(t *testing.T) {
	payload := make([]byte, inetDiagMsgSize+4+208)
	payload[0] = unix.AF_INET
	payload[1] = 1
	binary.NativeEndian.PutUint32(payload[40:44], 7)
	binary.NativeEndian.PutUint32(payload[44:48], 11)
	binary.NativeEndian.PutUint32(payload[48:52], 12)
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
