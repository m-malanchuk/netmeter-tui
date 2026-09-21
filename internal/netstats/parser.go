package netstats

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const procNetDevFields = 16

// Parse reads the contents of /proc/net/dev from r.
//
// Header and blank lines are ignored. Interface rows must contain the standard
// sixteen Linux counters; only RX bytes (field 0) and TX bytes (field 8) are
// retained.
func Parse(r io.Reader) ([]Counters, error) {
	scanner := bufio.NewScanner(r)
	interfaces := make([]Counters, 0, 8)
	seen := make(map[string]struct{})
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.Contains(line, ":") {
			continue
		}

		namePart, valuesPart, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name := strings.TrimSpace(namePart)
		if name == "" {
			return nil, fmt.Errorf("line %d: empty interface name", lineNumber)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("line %d: duplicate interface %q", lineNumber, name)
		}

		fields := strings.Fields(valuesPart)
		if len(fields) < procNetDevFields {
			return nil, fmt.Errorf("line %d interface %q: expected at least %d counters, got %d", lineNumber, name, procNetDevFields, len(fields))
		}
		rxBytes, err := parseCounter(fields[0], lineNumber, name, "RX")
		if err != nil {
			return nil, err
		}
		txBytes, err := parseCounter(fields[8], lineNumber, name, "TX")
		if err != nil {
			return nil, err
		}

		seen[name] = struct{}{}
		interfaces = append(interfaces, Counters{Name: name, RXBytes: rxBytes, TXBytes: txBytes})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan /proc/net/dev: %w", err)
	}

	sort.Slice(interfaces, func(i, j int) bool {
		return interfaces[i].Name < interfaces[j].Name
	})
	return interfaces, nil
}

func parseCounter(value string, lineNumber int, name string, direction string) (uint64, error) {
	counter, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("line %d interface %q: invalid %s byte counter %q: %w", lineNumber, name, direction, value, err)
	}
	return counter, nil
}
