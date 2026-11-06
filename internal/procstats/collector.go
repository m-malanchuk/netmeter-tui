package procstats

import (
	"context"
	"errors"
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
// processes. TCP counters come from inet_diag. UDP bytes are accumulated by
// eBPF when available, with best-effort AF_PACKET capture as a fallback.
type Collector struct {
	procRoot            string
	previous            map[socketKey]socketCounters
	mode                string
	udpBPF              *udpBPFCounter
	udpPrevious         map[udpBPFKey]udpBPFValue
	udpOwners           map[uint64]udpBPFIdentity
	udpBPFStale         map[udpBPFKey]uint8
	udp                 *udpCapture
	udpCaptureAttempted bool
	udpCaptureWarning   collectorWarning
	udpInterfaces       map[uint64]string
}

// NewCollector creates a collector backed by the host's procfs.
func NewCollector() *Collector {
	return &Collector{procRoot: defaultProcRoot, previous: make(map[socketKey]socketCounters), udpPrevious: make(map[udpBPFKey]udpBPFValue), udpOwners: make(map[uint64]udpBPFIdentity), udpBPFStale: make(map[udpBPFKey]uint8), udpInterfaces: make(map[uint64]string)}
}

// NewCollectorFromProcRoot creates a collector using a custom procfs root.
// The netlink socket remains connected to the running Linux kernel; the custom
// root is useful for deterministic ownership-mapping tests.
func NewCollectorFromProcRoot(root string) *Collector {
	return &Collector{procRoot: root, previous: make(map[socketKey]socketCounters), udpPrevious: make(map[udpBPFKey]udpBPFValue), udpOwners: make(map[uint64]udpBPFIdentity), udpBPFStale: make(map[udpBPFKey]uint8), udpInterfaces: make(map[uint64]string)}
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
	warnings := make([]collectorWarning, 0, 3)
	sockets, mode, tcpWarning, err := collectTCPSockets(ctx, c.procRoot)
	if err != nil {
		return Snapshot{}, err
	}
	if tcpWarning.code != 0 {
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
			warnings = append(warnings, collectorWarning{code: warningOwnerRefresh, detail: refreshErr.Error()})
		}
	}

	now := time.Now()
	current := make(map[socketKey]socketCounters, len(sockets))
	aggregates := make(map[processKey]*ProcessCounters)
	for _, socket := range sockets {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		interfaceName := resolver.resolve(socket)
		owner, found := owners[socket.inode]
		if !found {
			owner = classifyUnownedSocket(socket.uid, socket.uidKnown, socket.local, socket.remote, socket.localPort, resolver)
		}
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

		interfaceName := resolver.resolve(socket)
		owner, found := owners[socket.inode]
		if !found {
			owner = classifyUnownedSocket(socket.uid, socket.uidKnown, socket.local, socket.remote, socket.localPort, resolver)
		}
		aggregate := ensureAggregate(aggregates, owner, interfaceName)
		aggregate.RXBytes = saturatingAdd(aggregate.RXBytes, rxDelta)
		aggregate.TXBytes = saturatingAdd(aggregate.TXBytes, txDelta)
	}
	c.previous = current

	if udpErr != nil {
		warnings = append(warnings, collectorWarning{code: warningUDPSocketTable, detail: udpErr.Error()})
	} else {
		for _, socket := range udpSockets {
			if err := ctx.Err(); err != nil {
				return Snapshot{}, err
			}
			interfaceName := resolver.resolveUDP(socket)
			if interfaceName == "" {
				interfaceName = c.udpInterfaces[socket.inode]
			}
			owner, found := owners[socket.inode]
			if !found {
				owner = classifyUnownedSocket(socket.uid, socket.uidKnown, socket.local, socket.remote, socket.localPort, resolver)
			}
			aggregate := ensureAggregate(aggregates, owner, interfaceName)
			aggregate.Connections++
		}

		c.ensureUDPCapture()
		if c.udpCaptureWarning.code != 0 {
			warnings = append(warnings, c.udpCaptureWarning)
		}
		if c.udpBPF != nil || c.udp != nil {
			var traffic map[processKey]udpTraffic
			var warning collectorWarning
			var err error
			if c.udpBPF != nil {
				traffic, warning, err = c.collectBPFUDP(ctx, udpSockets, owners, resolver)
			} else {
				traffic, err = c.udp.collect(ctx, udpSockets, owners, resolver)
			}
			if warning.code != 0 {
				warnings = append(warnings, warning)
			}
			if err != nil {
				if c.udpBPF != nil && ctx.Err() == nil {
					c.fallbackToPacketCapture(err)
				}
				if c.udpCaptureWarning.code != 0 {
					warnings = append(warnings, c.udpCaptureWarning)
				} else {
					warnings = append(warnings, collectorWarning{code: warningUDPAccounting, detail: "UDP byte accounting error: " + err.Error()})
				}
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
		c.pruneUDPInterfaces(udpSockets)
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

func joinWarnings(warnings []collectorWarning) string {
	parts := make([]string, 0, len(warnings))
	seen := make(map[string]struct{}, len(warnings))
	for _, warning := range warnings {
		message := strings.TrimSpace(warning.message())
		if message == "" {
			continue
		}
		key := fmt.Sprintf("%d:%s", warning.code, message)
		if _, found := seen[key]; found {
			continue
		}
		seen[key] = struct{}{}
		parts = append(parts, message)
	}
	return strings.Join(parts, "; ")
}

func (c *Collector) ensureUDPCapture() {
	if c.udpCaptureAttempted {
		return
	}
	c.udpCaptureAttempted = true
	bpfCounter, bpfErr := newUDPBPFCounter()
	if bpfErr == nil {
		c.udpBPF = bpfCounter
		return
	}
	bpfReason := bpfErr.Error()
	if errors.Is(bpfErr, unix.EPERM) || errors.Is(bpfErr, unix.EACCES) {
		bpfReason = "permission denied; CAP_BPF and CAP_NET_ADMIN are required"
	}
	capture, err := newUDPCapture()
	c.udp = capture
	if err != nil {
		c.udpCaptureWarning = collectorWarning{code: warningUDPFallback, detail: fmt.Sprintf("UDP byte counters unavailable (eBPF: %s; packet capture: %v); showing UDP socket counts only", bpfReason, err)}
	} else {
		c.udpCaptureWarning = collectorWarning{code: warningUDPFallback, detail: fmt.Sprintf("UDP eBPF unavailable (%s); using best-effort packet capture", bpfReason)}
	}
}

func (c *Collector) fallbackToPacketCapture(cause error) {
	if c.udpBPF != nil {
		_ = c.udpBPF.close()
		c.udpBPF = nil
	}
	capture, err := newUDPCapture()
	if err != nil {
		c.udp = nil
		c.udpCaptureWarning = collectorWarning{code: warningUDPFallback, detail: fmt.Sprintf("UDP eBPF failed (%v); packet capture unavailable (%v); showing UDP socket counts only", cause, err)}
		return
	}
	c.udp = capture
	c.udpCaptureWarning = collectorWarning{code: warningUDPFallback, detail: fmt.Sprintf("UDP eBPF failed (%v); using best-effort packet capture", cause)}
}

func (c *Collector) pruneUDPInterfaces(sockets []udpSocket) {
	active := make(map[uint64]struct{}, len(sockets))
	for _, socket := range sockets {
		active[socket.inode] = struct{}{}
	}
	for inode := range c.udpInterfaces {
		if _, found := active[inode]; !found {
			delete(c.udpInterfaces, inode)
		}
	}
}

// Close releases the optional UDP accounting resources.
func (c *Collector) Close() error {
	var first error
	if c.udpBPF != nil {
		first = c.udpBPF.close()
		c.udpBPF = nil
	}
	if c.udp != nil {
		if err := c.udp.close(); err != nil && first == nil {
			first = err
		}
		c.udp = nil
	}
	return first
}

func collectTCPSockets(ctx context.Context, root string) ([]diagSocket, string, collectorWarning, error) {
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
			return nil, "", collectorWarning{}, ctx.Err()
		}
		fallback, fallbackErr := readProcSocketFamily(ctx, root, item.family)
		if fallbackErr != nil {
			return nil, "", collectorWarning{}, fmt.Errorf("query TCP %s sockets: %w (proc fallback: %v)", item.name, diagErr, fallbackErr)
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
	warning := collectorWarning{}
	if procFamilies > 0 {
		warning = collectorWarning{code: warningTCPFallback, detail: strings.Join(failedFamilies, "; ")}
	}
	return result, mode, warning, nil
}
