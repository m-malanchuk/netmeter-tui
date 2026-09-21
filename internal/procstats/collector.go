package procstats

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const defaultProcRoot = "/proc"

const tcpDiagTimeout = 3 * time.Second

type socketCounters struct {
	rx uint64
	tx uint64
}

// Collector reads TCP and UDP socket observations and resolves their owning
// processes. TCP counters come from inet_diag. UDP payload bytes are observed
// with a best-effort AF_PACKET capture and are unavailable without the
// CAP_NET_RAW capability; UDP socket rows remain visible in that case.
type Collector struct {
	procRoot            string
	previous            map[socketKey]socketCounters
	mode                string
	udp                 *udpCapture
	udpCaptureAttempted bool
	udpCaptureWarning   string
	udpInterfaces       map[uint64]string
}

// NewCollector creates a collector backed by the host's procfs.
func NewCollector() *Collector {
	return &Collector{procRoot: defaultProcRoot, previous: make(map[socketKey]socketCounters), udpInterfaces: make(map[uint64]string)}
}

// NewCollectorFromProcRoot creates a collector using a custom procfs root.
// The netlink socket remains connected to the running Linux kernel; the custom
// root is useful for deterministic ownership-mapping tests.
func NewCollectorFromProcRoot(root string) *Collector {
	return &Collector{procRoot: root, previous: make(map[socketKey]socketCounters), udpInterfaces: make(map[uint64]string)}
}

// Collect returns per-process TCP/UDP observations for the current instant.
func (c *Collector) Collect(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	owners, err := readProcessOwners(ctx, c.procRoot)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read process sockets: %w", err)
	}
	resolver := newInterfaceResolver()
	warnings := make([]string, 0, 3)
	sockets, mode, tcpWarning, err := collectTCPSockets(ctx, c.procRoot)
	if err != nil {
		return Snapshot{}, err
	}
	if tcpWarning != "" {
		warnings = append(warnings, tcpWarning)
	}
	if c.mode != "" && c.mode != mode {
		c.previous = make(map[socketKey]socketCounters)
	}
	c.mode = mode
	udpSockets, udpErr := readProcUDPSockets(ctx, c.procRoot)
	if ownersNeedRefresh(owners, sockets, udpSockets) {
		refreshedOwners, refreshErr := readProcessOwners(ctx, c.procRoot)
		if refreshErr == nil {
			mergeProcessOwners(owners, refreshedOwners)
		} else if ctx.Err() != nil {
			return Snapshot{}, ctx.Err()
		} else {
			warnings = append(warnings, "process owner refresh unavailable: "+refreshErr.Error())
		}
	}

	now := time.Now()
	current := make(map[socketKey]socketCounters, len(sockets))
	aggregates := make(map[processKey]*ProcessCounters)
	for _, socket := range sockets {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		owner, found := owners[socket.inode]
		if !found {
			owner = processOwner{name: "unknown"}
		}
		interfaceName := resolver.resolve(socket)
		aggregate := ensureAggregate(aggregates, owner, interfaceName)
		aggregate.Connections++
	}
	for _, socket := range sockets {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		key := socket.key()
		current[key] = socketCounters{rx: socket.rxBytes, tx: socket.txBytes}
		previous, seen := c.previous[key]
		rxDelta, txDelta := uint64(0), uint64(0)
		if seen {
			if socket.rxBytes >= previous.rx {
				rxDelta = socket.rxBytes - previous.rx
			}
			if socket.txBytes >= previous.tx {
				txDelta = socket.txBytes - previous.tx
			}
		}

		owner, found := owners[socket.inode]
		if !found {
			owner = processOwner{name: "unknown"}
		}
		interfaceName := resolver.resolve(socket)
		aggregate := ensureAggregate(aggregates, owner, interfaceName)
		aggregate.RXBytes = saturatingAdd(aggregate.RXBytes, rxDelta)
		aggregate.TXBytes = saturatingAdd(aggregate.TXBytes, txDelta)
	}
	c.previous = current

	if udpErr != nil {
		warnings = append(warnings, "UDP socket table unavailable: "+udpErr.Error())
	} else {
		for _, socket := range udpSockets {
			if err := ctx.Err(); err != nil {
				return Snapshot{}, err
			}
			owner, found := owners[socket.inode]
			if !found {
				owner = processOwner{name: "unknown"}
			}
			interfaceName := resolver.resolveUDP(socket)
			if interfaceName == "" {
				interfaceName = c.udpInterfaces[socket.inode]
			}
			aggregate := ensureAggregate(aggregates, owner, interfaceName)
			aggregate.Connections++
		}

		c.ensureUDPCapture()
		if c.udp == nil {
			warnings = append(warnings, c.udpCaptureWarning)
		} else {
			traffic, err := c.udp.collect(ctx, udpSockets, owners, resolver)
			if err != nil {
				warnings = append(warnings, "UDP packet capture error: "+err.Error())
			} else {
				for key, bytes := range traffic {
					aggregate := aggregates[key]
					if aggregate == nil && key.interfaceName != "" {
						aggregate = moveUnresolvedAggregate(aggregates, key)
					}
					if aggregate == nil {
						aggregate = &ProcessCounters{PID: key.pid, StartTime: key.startTime, Interface: key.interfaceName}
						if owner, found := owners[bytes.inode]; found {
							aggregate.Name = owner.name
						} else {
							aggregate.Name = "unknown"
						}
						aggregates[key] = aggregate
					}
					if key.interfaceName != "" {
						c.udpInterfaces[bytes.inode] = key.interfaceName
					}
					aggregate.RXBytes = saturatingAdd(aggregate.RXBytes, bytes.rx)
					aggregate.TXBytes = saturatingAdd(aggregate.TXBytes, bytes.tx)
				}
			}
		}
	}

	result := make([]ProcessCounters, 0, len(aggregates))
	for _, counters := range aggregates {
		result = append(result, *counters)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Interface != result[j].Interface {
			return result[i].Interface < result[j].Interface
		}
		if result[i].PID != result[j].PID {
			return result[i].PID < result[j].PID
		}
		return result[i].Name < result[j].Name
	})
	return Snapshot{At: now, Processes: result, Warning: joinWarnings(warnings)}, nil
}

func mergeProcessOwners(destination, source map[uint64]processOwner) {
	for inode, owner := range source {
		if current, found := destination[inode]; !found || current.pid <= 0 || (owner.pid > 0 && owner.pid < current.pid) {
			destination[inode] = owner
		}
	}
}

func ownersNeedRefresh(owners map[uint64]processOwner, tcpSockets []diagSocket, udpSockets []udpSocket) bool {
	for _, socket := range tcpSockets {
		if socket.inode != 0 {
			if _, found := owners[socket.inode]; !found {
				return true
			}
		}
	}
	for _, socket := range udpSockets {
		if socket.inode != 0 {
			if _, found := owners[socket.inode]; !found {
				return true
			}
		}
	}
	return false
}

func ensureAggregate(aggregates map[processKey]*ProcessCounters, owner processOwner, interfaceName string) *ProcessCounters {
	key := processKey{pid: owner.pid, startTime: owner.startTime, interfaceName: interfaceName}
	aggregate := aggregates[key]
	if aggregate == nil {
		aggregate = &ProcessCounters{
			PID: owner.pid, StartTime: owner.startTime, Name: owner.name, Interface: interfaceName,
		}
		aggregates[key] = aggregate
	}
	return aggregate
}

func moveUnresolvedAggregate(aggregates map[processKey]*ProcessCounters, target processKey) *ProcessCounters {
	for key, aggregate := range aggregates {
		if key.pid != target.pid || key.startTime != target.startTime || key.interfaceName != "" {
			continue
		}
		delete(aggregates, key)
		aggregate.Interface = target.interfaceName
		aggregates[target] = aggregate
		return aggregate
	}
	return nil
}

func joinWarnings(warnings []string) string {
	parts := warnings[:0]
	for _, warning := range warnings {
		if strings.TrimSpace(warning) != "" {
			parts = append(parts, warning)
		}
	}
	return strings.Join(parts, "; ")
}

func (c *Collector) ensureUDPCapture() {
	if c.udpCaptureAttempted {
		return
	}
	c.udpCaptureAttempted = true
	capture, err := newUDPCapture()
	c.udp = capture
	if err != nil {
		c.udpCaptureWarning = fmt.Sprintf("UDP byte counters unavailable (%v; raw packet capture usually requires CAP_NET_RAW); showing UDP socket counts only", err)
	}
}

// Close releases the optional raw packet capture socket.
func (c *Collector) Close() error {
	if c.udp == nil {
		return nil
	}
	err := c.udp.close()
	c.udp = nil
	return err
}

func collectTCPSockets(ctx context.Context, root string) ([]diagSocket, string, string, error) {
	result := make([]diagSocket, 0)
	failedFamilies := make([]string, 0, 2)
	diagFamilies := 0
	procFamilies := 0
	for _, item := range []struct {
		name   string
		family uint8
	}{
		{name: "IPv4", family: unix.AF_INET},
		{name: "IPv6", family: unix.AF_INET6},
	} {
		diagContext, cancelDiag := context.WithTimeout(ctx, tcpDiagTimeout)
		sockets, diagErr := dumpTCPFamily(diagContext, item.family)
		cancelDiag()
		if diagErr == nil {
			result = append(result, sockets...)
			diagFamilies++
			continue
		}
		if ctx.Err() != nil {
			return nil, "", "", ctx.Err()
		}
		fallback, fallbackErr := readProcSocketFamily(ctx, root, item.family)
		if fallbackErr != nil {
			return nil, "", "", fmt.Errorf("query TCP %s sockets: %w (proc fallback: %v)", item.name, diagErr, fallbackErr)
		}
		result = append(result, fallback...)
		procFamilies++
		failedFamilies = append(failedFamilies, fmt.Sprintf("%s: %v", item.name, diagErr))
	}

	mode := "proc"
	if diagFamilies == 2 {
		mode = "diag"
	} else if diagFamilies > 0 {
		mode = "mixed"
	}
	warning := ""
	if procFamilies > 0 {
		warning = "TCP byte counters unavailable (" + strings.Join(failedFamilies, "; ") + "); showing connection counts for affected families only"
	}
	return result, mode, warning, nil
}
