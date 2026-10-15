package procstats

import (
	"fmt"
	"os"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
	"golang.org/x/sys/unix"
)

const udpBPFMapEntries = 65536

type udpBPFKey struct {
	Cookie  uint64
	IfIndex uint32
	Padding uint32
}

type udpBPFValue struct {
	RX uint64
	TX uint64
}

// udpBPFCounter accumulates UDP IP-layer bytes in kernel maps. No packet data is
// queued for userspace, so a slow process scan cannot overflow a capture socket.
type udpBPFCounter struct {
	bytes       *ebpf.Map
	failures    *ebpf.Map
	ingress     *ebpf.Program
	egress      *ebpf.Program
	ingressLink link.Link
	egressLink  link.Link
}

func newUDPBPFCounter() (counter *udpBPFCounter, err error) {
	counter = &udpBPFCounter{}
	defer func() {
		if err != nil {
			counter.close()
			counter = nil
		}
	}()
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("raise BPF memlock limit: %w", err)
	}
	counter.bytes, err = ebpf.NewMap(&ebpf.MapSpec{
		Name: "nm_udp_bytes", Type: ebpf.Hash,
		KeySize: 16, ValueSize: 16, MaxEntries: udpBPFMapEntries,
	})
	if err != nil {
		return nil, fmt.Errorf("create UDP byte map: %w", err)
	}
	counter.failures, err = ebpf.NewMap(&ebpf.MapSpec{
		Name: "nm_udp_fail", Type: ebpf.Array,
		KeySize: 4, ValueSize: 8, MaxEntries: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("create UDP failure map: %w", err)
	}
	counter.ingress, err = ebpf.NewProgram(&ebpf.ProgramSpec{
		Name: "netmeter_udp_rx", Type: ebpf.CGroupSKB,
		AttachType:   ebpf.AttachCGroupInetIngress,
		Instructions: udpBPFInstructions(counter.bytes.FD(), counter.failures.FD(), false),
		License:      "MIT",
	})
	if err != nil {
		return nil, fmt.Errorf("load UDP ingress program: %w", err)
	}
	counter.egress, err = ebpf.NewProgram(&ebpf.ProgramSpec{
		Name: "netmeter_udp_tx", Type: ebpf.CGroupSKB,
		AttachType:   ebpf.AttachCGroupInetEgress,
		Instructions: udpBPFInstructions(counter.bytes.FD(), counter.failures.FD(), true),
		License:      "MIT",
	})
	if err != nil {
		return nil, fmt.Errorf("load UDP egress program: %w", err)
	}
	const cgroupPath = "/sys/fs/cgroup"
	if _, err = os.Stat(cgroupPath); err != nil {
		return nil, fmt.Errorf("open cgroup root: %w", err)
	}
	counter.ingressLink, err = link.AttachCgroup(link.CgroupOptions{
		Path: cgroupPath, Attach: ebpf.AttachCGroupInetIngress, Program: counter.ingress,
	})
	if err != nil {
		return nil, fmt.Errorf("attach UDP ingress program: %w", err)
	}
	counter.egressLink, err = link.AttachCgroup(link.CgroupOptions{
		Path: cgroupPath, Attach: ebpf.AttachCGroupInetEgress, Program: counter.egress,
	})
	if err != nil {
		return nil, fmt.Errorf("attach UDP egress program: %w", err)
	}
	return counter, nil
}

func (counter *udpBPFCounter) close() error {
	if counter == nil {
		return nil
	}
	var first error
	closeResource := func(closer interface{ Close() error }) {
		if err := closer.Close(); err != nil && first == nil {
			first = err
		}
	}
	if counter.egressLink != nil {
		closeResource(counter.egressLink)
	}
	if counter.ingressLink != nil {
		closeResource(counter.ingressLink)
	}
	if counter.egress != nil {
		closeResource(counter.egress)
	}
	if counter.ingress != nil {
		closeResource(counter.ingress)
	}
	if counter.failures != nil {
		closeResource(counter.failures)
	}
	if counter.bytes != nil {
		closeResource(counter.bytes)
	}
	return first
}

func (counter *udpBPFCounter) snapshot() (map[udpBPFKey]udpBPFValue, uint64, error) {
	values := make(map[udpBPFKey]udpBPFValue)
	iterator := counter.bytes.Iterate()
	var key udpBPFKey
	var value udpBPFValue
	for iterator.Next(&key, &value) {
		values[key] = value
	}
	if err := iterator.Err(); err != nil {
		return nil, 0, fmt.Errorf("read UDP byte map: %w", err)
	}
	var failures uint64
	if err := counter.failures.Lookup(uint32(0), &failures); err != nil {
		return nil, 0, fmt.Errorf("read UDP failure map: %w", err)
	}
	return values, failures, nil
}

func (counter *udpBPFCounter) delete(key udpBPFKey) error {
	if counter == nil || counter.bytes == nil {
		return nil
	}
	return counter.bytes.Delete(key)
}

func udpBPFInstructions(bytesFD, failuresFD int, outbound bool) asm.Instructions {
	interfaceOffset := int16(36) // __sk_buff.ingress_ifindex
	valueOffset := int16(0)      // RX
	if outbound {
		interfaceOffset = 40 // __sk_buff.ifindex
		valueOffset = 8      // TX
	}
	return asm.Instructions{
		asm.Mov.Reg(asm.R6, asm.R1),
		// Read relative to the network header because skb->data can differ
		// between ingress and egress. Ordinary IPv4 and IPv6 UDP datagrams
		// have their protocol byte at offsets 9 and 6.
		asm.Mov.Reg(asm.R1, asm.R6),
		asm.Mov.Imm(asm.R2, 0),
		asm.Mov.Reg(asm.R3, asm.RFP),
		asm.Add.Imm(asm.R3, -64),
		asm.Mov.Imm(asm.R4, 10),
		asm.Mov.Imm(asm.R5, unix.BPF_HDR_START_NET),
		asm.FnSkbLoadBytesRelative.Call(),
		asm.JNE.Imm(asm.R0, 0, "allow"),
		asm.LoadMem(asm.R0, asm.RFP, -64, asm.Byte),
		asm.RSh.Imm(asm.R0, 4),
		asm.JEq.Imm(asm.R0, 4, "ipv4"),
		asm.JEq.Imm(asm.R0, 6, "ipv6"),
		asm.Ja.Label("allow"),
		asm.LoadMem(asm.R0, asm.RFP, -55, asm.Byte).WithSymbol("ipv4"),
		asm.JNE.Imm(asm.R0, udpProtocol, "allow"),
		asm.Ja.Label("count"),
		asm.LoadMem(asm.R0, asm.RFP, -58, asm.Byte).WithSymbol("ipv6"),
		asm.JNE.Imm(asm.R0, udpProtocol, "allow"),
		asm.Mov.Reg(asm.R1, asm.R6).WithSymbol("count"),
		asm.FnGetSocketCookie.Call(),
		asm.JEq.Imm(asm.R0, 0, "allow"),
		asm.StoreMem(asm.RFP, -32, asm.R0, asm.DWord),
		asm.LoadMem(asm.R7, asm.R6, 0, asm.Word), // __sk_buff.len
		asm.LoadMem(asm.R8, asm.R6, interfaceOffset, asm.Word),
		asm.StoreMem(asm.RFP, -24, asm.R8, asm.Word),
		asm.StoreImm(asm.RFP, -20, 0, asm.Word),
		asm.LoadMapPtr(asm.R1, bytesFD),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, -32),
		asm.FnMapLookupElem.Call(),
		asm.JNE.Imm(asm.R0, 0, "found"),
		asm.Mov.Imm(asm.R0, 0),
		asm.StoreMem(asm.RFP, -48, asm.R0, asm.DWord),
		asm.StoreMem(asm.RFP, -40, asm.R0, asm.DWord),
		asm.LoadMapPtr(asm.R1, bytesFD),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, -32),
		asm.Mov.Reg(asm.R3, asm.RFP),
		asm.Add.Imm(asm.R3, -48),
		asm.Mov.Imm(asm.R4, 1), // BPF_NOEXIST
		asm.FnMapUpdateElem.Call(),
		asm.LoadMapPtr(asm.R1, bytesFD),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, -32),
		asm.FnMapLookupElem.Call(),
		asm.JEq.Imm(asm.R0, 0, "failed"),
		asm.AddAtomic.Mem(asm.R0, asm.R7, asm.DWord, valueOffset).WithSymbol("found"),
		asm.Ja.Label("allow"),
		asm.StoreImm(asm.RFP, -52, 0, asm.Word).WithSymbol("failed"),
		asm.LoadMapPtr(asm.R1, failuresFD),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, -52),
		asm.FnMapLookupElem.Call(),
		asm.JEq.Imm(asm.R0, 0, "allow"),
		asm.Mov.Imm(asm.R1, 1),
		asm.AddAtomic.Mem(asm.R0, asm.R1, asm.DWord, 0),
		asm.Mov.Imm(asm.R0, 1).WithSymbol("allow"),
		asm.Return(),
	}
}
