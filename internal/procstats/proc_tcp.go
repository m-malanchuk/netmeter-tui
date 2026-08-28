package procstats

import (
	"context"
	"encoding/binary"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func readProcSocketFamily(ctx context.Context, root string, family uint8) ([]diagSocket, error) {
	path := filepath.Join(root, "net", "tcp")
	if family == unix.AF_INET6 {
		path = filepath.Join(root, "net", "tcp6")
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	lines, err := readLines(file)
	if err != nil {
		return nil, err
	}
	result := make([]diagSocket, 0, len(lines))
	for _, line := range lines[1:] {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if socket, ok := parseProcSocketLine(line, family); ok {
			result = append(result, socket)
		}
	}
	return result, nil
}

func readLines(file *os.File) ([]string, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n"), nil
}

func parseProcSocketLine(line string, family uint8) (diagSocket, bool) {
	fields := strings.Fields(line)
	if len(fields) < 11 {
		return diagSocket{}, false
	}
	local, ok := parseProcEndpoint(fields[1], family)
	if !ok {
		return diagSocket{}, false
	}
	remote, ok := parseProcEndpoint(fields[2], family)
	if !ok {
		return diagSocket{}, false
	}
	state, err := strconv.ParseUint(fields[3], 16, 8)
	if err != nil || state == 6 { // TIME_WAIT has no useful owning file descriptor.
		return diagSocket{}, false
	}
	inode, err := strconv.ParseUint(fields[9], 10, 64)
	if err != nil || inode == 0 {
		return diagSocket{}, false
	}
	return diagSocket{inode: inode, local: local, remote: remote}, true
}

func parseProcEndpoint(value string, family uint8) (netip.Addr, bool) {
	address, _, ok := parseProcEndpointPort(value, family)
	return address, ok
}

func parseProcEndpointPort(value string, family uint8) (netip.Addr, uint16, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return netip.Addr{}, 0, false
	}
	port, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return netip.Addr{}, 0, false
	}
	addressHex := parts[0]
	if family == unix.AF_INET && len(addressHex) == 8 {
		raw, err := strconv.ParseUint(addressHex, 16, 32)
		if err != nil {
			return netip.Addr{}, 0, false
		}
		var address [4]byte
		binary.LittleEndian.PutUint32(address[:], uint32(raw))
		return netip.AddrFrom4(address), uint16(port), true
	}
	if family == unix.AF_INET6 && len(addressHex) == 32 {
		var address [16]byte
		for offset := 0; offset < 16; offset += 4 {
			raw, err := strconv.ParseUint(addressHex[offset*2:offset*2+8], 16, 32)
			if err != nil {
				return netip.Addr{}, 0, false
			}
			binary.LittleEndian.PutUint32(address[offset:offset+4], uint32(raw))
		}
		return netip.AddrFrom16(address), uint16(port), true
	}
	return netip.Addr{}, 0, false
}
