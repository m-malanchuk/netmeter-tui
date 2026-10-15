package procstats

import (
	"bytes"
	"context"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestUDPCookieMatchesDiag(t *testing.T) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Skipf("UDP socket unavailable: %v", err)
	}
	defer unix.Close(fd)
	if err := unix.Bind(fd, &unix.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		t.Skipf("UDP bind unavailable: %v", err)
	}
	address, err := unix.Getsockname(fd)
	if err != nil {
		t.Fatal(err)
	}
	port := address.(*unix.SockaddrInet4).Port
	cookie, err := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_COOKIE)
	if err != nil {
		t.Skipf("SO_COOKIE unavailable: %v", err)
	}
	sockets, err := dumpSocketFamily(context.Background(), unix.AF_INET, udpProtocol)
	if err != nil {
		t.Skipf("UDP socket diagnostics unavailable: %v", err)
	}
	for _, socket := range sockets {
		if int(socket.localPort) == port {
			if got := diagSocketCookie(socket); got != cookie {
				t.Fatalf("diag cookie %#x != SO_COOKIE %#x", got, cookie)
			}
			return
		}
	}
	t.Fatalf("UDP socket port %d not found in diag dump", port)
}

func TestUDPBPFAttachmentIntegration(t *testing.T) {
	if os.Getenv("NETMETER_TUI_PRIVILEGED_BPF") != "1" {
		t.Skip("set NETMETER_TUI_PRIVILEGED_BPF=1 to run the cgroup/eBPF integration test")
	}
	counter, err := newUDPBPFCounter()
	if err != nil {
		t.Skipf("eBPF verifier or cgroup attachment unavailable: %v", err)
	}
	defer func() {
		if err := counter.close(); err != nil {
			t.Errorf("close eBPF counter: %v", err)
		}
	}()

	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skipf("UDP receiver unavailable: %v", err)
	}
	defer receiver.Close()
	sender, err := net.DialUDP("udp4", nil, receiver.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Skipf("UDP sender unavailable: %v", err)
	}
	defer sender.Close()

	payload := bytes.Repeat([]byte("netmeter"), 256)
	for index := 0; index < 3; index++ {
		if _, err := sender.Write(payload); err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		values, failures, err := counter.snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if failures != 0 {
			t.Fatalf("eBPF failure counter = %d", failures)
		}
		var sawRX, sawTX bool
		for _, value := range values {
			sawRX = sawRX || value.RX > 0
			sawTX = sawTX || value.TX > 0
		}
		if sawRX && sawTX {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("eBPF map did not observe both RX and TX generated UDP traffic")
}

func TestUDPAFPacketCaptureIntegration(t *testing.T) {
	if os.Getenv("NETMETER_TUI_LIVE_UDP_PACKET") != "1" {
		t.Skip("set NETMETER_TUI_LIVE_UDP_PACKET=1 to run the AF_PACKET integration test")
	}
	capture, err := newUDPCapture()
	if err != nil {
		t.Skipf("AF_PACKET capture unavailable: %v", err)
	}
	defer func() {
		if err := capture.close(); err != nil {
			t.Errorf("close AF_PACKET capture: %v", err)
		}
	}()

	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skipf("UDP receiver unavailable: %v", err)
	}
	defer receiver.Close()
	sender, err := net.DialUDP("udp4", nil, receiver.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Skipf("UDP sender unavailable: %v", err)
	}
	defer sender.Close()

	owners, err := readProcessOwners(context.Background(), defaultProcRoot)
	if err != nil {
		t.Fatal(err)
	}
	sockets, err := readProcUDPSockets(context.Background(), defaultProcRoot)
	if err != nil {
		t.Fatal(err)
	}
	resolver := newInterfaceResolver()
	payload := bytes.Repeat([]byte("netmeter"), 256)
	for index := 0; index < 3; index++ {
		if _, err := sender.Write(payload); err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		traffic, collectErr := capture.collect(context.Background(), sockets, owners, resolver)
		if collectErr != nil {
			t.Fatal(collectErr)
		}
		for _, value := range traffic {
			if value.rx > 0 || value.tx > 0 {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("AF_PACKET capture did not observe generated UDP traffic")
}
