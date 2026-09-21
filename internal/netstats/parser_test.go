package netstats

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

const fixture = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
  eth0: 1234567 1000 0 0 0 0 0 0 7654321 900 0 0 0 0 0 0
    lo: 500000 500 0 0 0 0 0 0 500000 500 0 0 0 0 0 0
`

func TestParse(t *testing.T) {
	got, err := Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []Counters{
		{Name: "eth0", RXBytes: 1234567, TXBytes: 7654321},
		{Name: "lo", RXBytes: 500000, TXBytes: 500000},
	}
	if len(got) != len(want) {
		t.Fatalf("Parse() length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Parse()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseErrors(t *testing.T) {
	tests := map[string]string{
		"empty name": " : 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16",
		"short row":  "eth0: 1 2 3",
		"invalid rx": "eth0: nope 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16",
		"invalid tx": "eth0: 1 2 3 4 5 6 7 8 nope 10 11 12 13 14 15 16",
		"duplicate":  "eth0: 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16\neth0: 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(input)); err == nil {
				t.Fatal("Parse() error = nil, want error")
			}
		})
	}
}

func TestCollectorFromPath(t *testing.T) {
	tempFile := t.TempDir() + "/dev"
	if err := writeFile(tempFile, fixture); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snapshot, err := NewCollectorFromPath(tempFile).Collect(ctx)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if snapshot.At.IsZero() {
		t.Fatal("Collect() returned zero timestamp")
	}
	if len(snapshot.Interfaces) != 2 {
		t.Fatalf("Collect() returned %d interfaces, want 2", len(snapshot.Interfaces))
	}
}

func TestCollectorHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewCollectorFromPath("/does/not/exist").Collect(ctx); err != context.Canceled {
		t.Fatalf("Collect() error = %v, want context canceled", err)
	}
}

func writeFile(path string, contents string) error {
	return os.WriteFile(path, []byte(contents), 0o600)
}
