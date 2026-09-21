package procstats

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type processOwner struct {
	pid       int
	startTime uint64
	name      string
}

func readProcessOwners(ctx context.Context, root string) (map[uint64]processOwner, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	owners := make(map[uint64]processOwner)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() || pid <= 0 {
			continue
		}
		processPath := filepath.Join(root, entry.Name())
		owner := processOwner{pid: pid, name: entry.Name()}
		if name, err := os.ReadFile(filepath.Join(processPath, "comm")); err == nil {
			owner.name = strings.TrimSpace(string(name))
		}
		if stat, err := os.ReadFile(filepath.Join(processPath, "stat")); err == nil {
			owner.startTime = parseStartTime(string(stat))
		}
		fds, err := os.ReadDir(filepath.Join(processPath, "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(processPath, "fd", fd.Name()))
			if err != nil {
				continue
			}
			inode, ok := parseSocketInode(link)
			if !ok {
				continue
			}
			if previous, exists := owners[inode]; !exists || owner.pid < previous.pid {
				owners[inode] = owner
			}
		}
	}
	return owners, nil
}

func parseStartTime(stat string) uint64 {
	end := strings.LastIndexByte(stat, ')')
	if end < 0 || end+1 >= len(stat) {
		return 0
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) <= 19 {
		return 0
	}
	value, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func parseSocketInode(link string) (uint64, bool) {
	const prefix = "socket:["
	if !strings.HasPrefix(link, prefix) || !strings.HasSuffix(link, "]") {
		return 0, false
	}
	value, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(link, prefix), "]"), 10, 64)
	return value, err == nil
}
