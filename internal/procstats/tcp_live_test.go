package procstats

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// TestTCPProcessAttributionIntegration is an opt-in live-kernel check. The
// ordinary suite remains deterministic and does not depend on traffic from the
// test machine; release verification can opt in explicitly when inet_diag and
// procfs are available.
func TestTCPProcessAttributionIntegration(t *testing.T) {
	if os.Getenv("NETMETER_TUI_LIVE_TCP") != "1" {
		t.Skip("set NETMETER_TUI_LIVE_TCP=1 to run the live TCP attribution test")
	}

	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skipf("TCP listener unavailable: %v", err)
	}
	defer listener.Close()

	accepted := make(chan net.Conn, 1)
	acceptErrors := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			acceptErrors <- acceptErr
			return
		}
		accepted <- connection
	}()

	client, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Skipf("TCP client unavailable: %v", err)
	}
	defer client.Close()

	var server net.Conn
	select {
	case server = <-accepted:
		defer server.Close()
	case acceptErr := <-acceptErrors:
		t.Skipf("TCP accept unavailable: %v", acceptErr)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for loopback TCP connection")
	}

	collector := NewCollector()
	defer func() {
		if err := collector.Close(); err != nil {
			t.Errorf("close process collector: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := collector.Collect(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "permission denied") {
			t.Skipf("TCP collection unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if strings.Contains(first.Warning, "TCP byte counters unavailable") {
		t.Skipf("TCP byte counters unavailable: %s", first.Warning)
	}

	payload := make([]byte, 128*1024)
	readResult := make(chan error, 1)
	go func() {
		_, readErr := io.CopyN(io.Discard, server, int64(len(payload)))
		readResult <- readErr
	}()
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := <-readResult; err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, collectErr := collector.Collect(ctx)
		if collectErr != nil {
			t.Fatal(collectErr)
		}
		for _, process := range snapshot.Processes {
			if process.PID == os.Getpid() && (process.RXBytes > 0 || process.TXBytes > 0) {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("loopback TCP traffic was not attributed to test process; baseline contained %d process rows", len(first.Processes))
}
