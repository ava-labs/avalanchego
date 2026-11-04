//go:build asmcore && (arm64 || amd64)

package asmevm

import (
	"bytes"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"github.com/holiman/uint256"
)

// slotBase is the address of dispatch slot 0 (slot n handles opcode n).
var slotBase uint64

var _ [len(slotAddr) - 256 - fusedCount]struct{} // every fused slot has an address (dispatch hints read slotAddr)

func init() {
	if !asmEVMAvailable {
		return
	}
	f := new(asmFrame)
	f.status = asmSlotCheck
	asmRun(f)
	if !slotsOK(f) {
		panic(fmt.Sprintf("asmevm: dispatch slots overflow (ret0=%#x ret1=%#x)", f.ret0, f.ret1))
	}
	slotBase = f.ret1
	jumpdestHandler = slotBase + 0x5b*128
	for k := range slotAddr {
		slotAddr[k] = slotBase + uint64(k)*128
	}
	predecodeHook = decodeCode
}

// decodeCode builds the pre-decoded stream (see decoded).
func decodeCode(code []byte) *decoded {
	n := len(code)
	d := &decoded{handlers: make([]uint64, n+33), args: make([]uint64, n+33)}
	big := 0
	for pc := 0; pc < n; pc++ {
		if op := code[pc]; op >= 0x60 && op <= 0x7f {
			if op >= 0x68 {
				big++
			}
			pc += int(op - 0x5f)
		}
	}
	d.consts = make([]uint256.Int, 0, big) // fixed capacity: args point into it
	stop, invalid := slotBase, slotBase+0xfe*128
	for i := n; i < n+33; i++ {
		d.handlers[i] = stop
	}
	for pc := 0; pc < n; {
		op := code[pc]
		d.handlers[pc] = slotBase + uint64(op)*128
		if op < 0x60 || op > 0x7f {
			pc++
			continue
		}
		size := int(op - 0x5f)
		var buf [32]byte
		copy(buf[32-size:], code[pc+1:min(pc+1+size, n)]) // missing bytes are zero
		var v uint256.Int
		v.SetBytes32(buf[:])
		if size <= 8 {
			d.args[pc] = v[0]
		} else {
			d.consts = append(d.consts, v)
			d.args[pc] = uint64(uintptr(unsafe.Pointer(&d.consts[len(d.consts)-1])))
		}
		for j := pc + 1; j <= pc+size && j < n; j++ {
			d.handlers[j] = invalid
		}
		pc += size + 1
	}
	if fuseEnabled {
		fuse(d, code)
	}
	// Diagnosis (ASMEVM_HOSTOPS): run the listed opcodes through the host, via
	// the slot of the undefined opcode 0x0c (a plain jump to host).
	for pc := 0; pc < n; pc++ {
		if op := code[pc]; hostOps[op] && d.handlers[pc] == slotBase+uint64(op)*128 {
			d.handlers[pc] = slotBase + 0x0c*128
		}
	}
	return d
}

// Fused slot indices (see gen_slots.py: slot 256+k).
const (
	fusedPush2Jumpi = iota
	fusedPush2Jump
	fusedIszeroPush2Jumpi
	fusedPush1Push1Shl
	fusedEqJumpi
	fusedLtJumpi
	fusedGtJumpi
	fusedAndJumpi
	fusedPush4EqJumpi
	// Stack-shuffle sequences from the 10,000-block training window
	// (96646201-96656200); the ...Jump ones finish in op_jump.
	fusedSwap1PopJump   // SWAP1 POP JUMP
	fusedSwap1Pop       // SWAP1 POP
	fusedPopPopJump     // POP POP JUMP
	fusedPopPop         // POP POP
	fusedPopJump        // POP JUMP
	fusedSwap2Swap1Pop  // SWAP2 SWAP1 POP
	fusedSwap2Swap1     // SWAP2 SWAP1
	fusedSwap2Pop       // SWAP2 POP
	fusedPopSwap2Swap1  // POP SWAP2 SWAP1
	fusedDup2Swap1Pop   // DUP2 SWAP1 POP
	fusedPush0Dup2Swap1 // PUSH0 DUP2 SWAP1
	fusedSwap3Pop       // SWAP3 POP
	fusedDup2Swap1      // DUP2 SWAP1
	fusedPush1Add       // PUSH1 x ADD
	fusedCount
)

// fusedSeqs are the plain-op superinstructions, longest first at each pc.
var fusedSeqs = []struct {
	k   int
	ops []byte
}{
	{fusedSwap1PopJump, []byte{0x90, 0x50, 0x56}},
	{fusedPopPopJump, []byte{0x50, 0x50, 0x56}},
	{fusedSwap2Swap1Pop, []byte{0x91, 0x90, 0x50}},
	{fusedPopSwap2Swap1, []byte{0x50, 0x91, 0x90}},
	{fusedDup2Swap1Pop, []byte{0x81, 0x90, 0x50}},
	{fusedPush0Dup2Swap1, []byte{0x5f, 0x81, 0x90}},
	{fusedSwap1Pop, []byte{0x90, 0x50}},
	{fusedPopPop, []byte{0x50, 0x50}},
	{fusedPopJump, []byte{0x50, 0x56}},
	{fusedSwap2Swap1, []byte{0x91, 0x90}},
	{fusedSwap2Pop, []byte{0x91, 0x50}},
	{fusedSwap3Pop, []byte{0x92, 0x50}},
	{fusedDup2Swap1, []byte{0x81, 0x90}},
}

// fuse rewrites the handler of the first op of each superinstruction pattern.
// The inner ops keep their own records, so a host fallback at the first op
// continues natively. Jump patterns are fused only when the destination is a
// valid JUMPDEST, so the fused handler skips the runtime check.
func fuse(d *decoded, code []byte) {
	n := len(code)
	jumpdest := slotBase + 0x5b*128
	isJD := func(dest uint64) bool { return dest < uint64(n) && d.handlers[dest] == jumpdest }
	h := func(k int) uint64 { return slotBase + uint64(256+k)*128 }
	folds := 0
	for pc := 0; pc+4 < n; pc++ {
		if code[pc] == 0x60 && code[pc+2] == 0x60 && code[pc+4] == 0x1b && d.handlers[pc] == slotBase+0x60*128 {
			folds++
		}
	}
	folded := make([]uint256.Int, 0, folds) // fixed capacity: args point into it
	d.consts2 = folded
	for pc := 0; pc < n; {
		op := code[pc]
		w := 1
		if op >= 0x60 && op <= 0x7f {
			w += int(op - 0x5f)
		}
		on := func(k int) bool { return k < archFused && fuseMask&(1<<k) != 0 }
		switch {
		case on(fusedPush4EqJumpi) && op == 0x63 && pc+9 < n && code[pc+5] == 0x14 && code[pc+6] == 0x61 && code[pc+9] == 0x57 && isJD(d.args[pc+6]):
			d.handlers[pc] = h(fusedPush4EqJumpi)
			w = 10
		case pc+4 < n && code[pc+1] == 0x61 && code[pc+4] == 0x57 && isJD(d.args[pc+1]) &&
			(on(fusedEqJumpi) && op == 0x14 || on(fusedLtJumpi) && op == 0x10 || on(fusedGtJumpi) && op == 0x11 || on(fusedAndJumpi) && op == 0x16):
			k := map[byte]int{0x14: fusedEqJumpi, 0x10: fusedLtJumpi, 0x11: fusedGtJumpi, 0x16: fusedAndJumpi}[op]
			d.handlers[pc], d.args[pc] = h(k), d.args[pc+1]
			w = 5
		case on(fusedPush2Jumpi) && op == 0x61 && pc+3 < n && code[pc+3] == 0x57 && isJD(d.args[pc]):
			d.handlers[pc] = h(fusedPush2Jumpi)
			w = 4
		case on(fusedPush2Jump) && op == 0x61 && pc+3 < n && code[pc+3] == 0x56 && isJD(d.args[pc]):
			d.handlers[pc] = h(fusedPush2Jump)
			w = 4
		case on(fusedIszeroPush2Jumpi) && op == 0x15 && pc+4 < n && code[pc+1] == 0x61 && code[pc+4] == 0x57 && isJD(d.args[pc+1]):
			d.handlers[pc], d.args[pc] = h(fusedIszeroPush2Jumpi), d.args[pc+1]
			w = 5
		case on(fusedPush1Push1Shl) && op == 0x60 && pc+4 < n && code[pc+2] == 0x60 && code[pc+4] == 0x1b:
			var v uint256.Int // PUSH1 a; PUSH1 b; SHL = a << b
			v.SetUint64(d.args[pc]).Lsh(&v, uint(d.args[pc+2]))
			d.consts2 = append(d.consts2, v)
			d.handlers[pc] = h(fusedPush1Push1Shl)
			d.args[pc] = uint64(uintptr(unsafe.Pointer(&d.consts2[len(d.consts2)-1])))
			w = 5
		case on(fusedPush1Add) && op == 0x60 && pc+2 < n && code[pc+2] == 0x01:
			d.handlers[pc] = h(fusedPush1Add) // args[pc] keeps the PUSH1 value
			w = 3
		default:
			for _, f := range fusedSeqs {
				if on(f.k) && pc+len(f.ops) <= n && string(code[pc:pc+len(f.ops)]) == string(f.ops) {
					d.handlers[pc] = h(f.k)
					w = len(f.ops)
					break
				}
			}
		}
		pc += w
	}
}

var asmEVMForced = false

// asmRun executes the frame natively, calling asmHost for ops it does not
// implement in assembly. Defined in core_arm64.s and core_amd64.s.
func asmRun(f *asmFrame)

func newAsmFrame() *asmFrame {
	f := new(asmFrame)
	f.stack.data = make([]uint256.Int, 0, 1024)
	f.memory.store = make([]byte, 0, asmMemInit)
	return f
}

// hostStats, when non-nil, counts host callbacks per opcode (index 256 = native
// frame entries). Profiling only; single goroutine.
var hostStats *[257]uint64

// SetHostStats turns callback counting on (non-nil) or off.
func SetHostStats(s *[257]uint64) { hostStats = s }

// hostNanos, when non-nil, accumulates the wall time spent inside asmHost (the
// Go side of a callback; the asm spill/reload and ABI wrappers are outside)
// for callbacks other than CALL/CREATE-family ops, whose time includes the
// nested frames; hostTimed counts those callbacks.
var (
	hostNanos *int64
	hostTimed int64
)

// With the timer on, CALL-family callbacks are timed too, minus the time of
// the native frames they run (asmRun of the child, including its own
// callbacks): callNanos is the call overhead (Go call setup, value transfer,
// snapshot, frame setup and teardown, return data copy) of callTimed calls.
var (
	callNanos, callTimed int64
	childNanos           int64 // asmRun time of the frames under the current call
)

// HostCallTiming returns and resets the CALL-family overhead counters.
func HostCallTiming() (calls, nanos int64) {
	calls, nanos = callTimed, callNanos
	callTimed, callNanos = 0, 0
	return
}

// HostOpNanos accumulates the timed host time per opcode (timer on).
var HostOpNanos [256]int64

// SetHostTiming turns the asmHost timer on (non-nil) or off and returns the
// number of timed callbacks since the previous call.
func SetHostTiming(p *int64) int64 {
	n := hostTimed
	hostNanos, hostTimed = p, 0
	return n
}

func nestsFrames(op byte) bool {
	switch op {
	case 0xf0, 0xf1, 0xf2, 0xf4, 0xf5, 0xfa:
		return true
	}
	return false
}

// frameAt returns the frame record of depth d, from the State's free list when
// new. The list is per State, not a sync.Pool: a frame's memory capacity
// decides whether compiled code bails on a memory expansion, so which frame a
// depth gets must not depend on GC or on the P the goroutine runs on.
func (in *Interpreter) frameAt(d int) *asmFrame {
	for len(in.frames) <= d {
		in.frames = append(in.frames, nil)
	}
	f := in.frames[d]
	if f == nil {
		if st := in.evm.state; len(st.frames) > 0 {
			f, st.frames = st.frames[len(st.frames)-1], st.frames[:len(st.frames)-1]
		} else {
			f = newAsmFrame()
		}
		f.in = in
		f.stackBase = unsafe.Pointer(unsafe.SliceData(f.stack.data))
		f.mem, f.memCap = unsafe.Pointer(unsafe.SliceData(f.memory.store)), uint64(cap(f.memory.store))
		in.frames[d] = f
		// Link the neighbours: a native CALL from depth d-1 enters f.
		if d > 0 && in.frames[d-1] != nil {
			in.frames[d-1].next = f
		}
		if d+1 < len(in.frames) {
			f.next = in.frames[d+1]
		}
	}
	return f
}

// releaseFrames returns the frame records to the State's free list (outermost
// runAsm), deepest first, so each depth gets its own record back next time.
func (in *Interpreter) releaseFrames() {
	for i := len(in.frames) - 1; i >= 0; i-- {
		f := in.frames[i]
		if f == nil {
			continue
		}
		f.in, f.contract, f.res, f.err, f.scope = nil, nil, nil, nil, scopeContext{}
		f.code, f.input, f.jumpdests, f.abort = nil, nil, nil, nil
		f.handlers, f.args, f.kcache, f.stab, f.nj = nil, nil, nil, nil, nil
		f.ctab, f.journal, f.next, f.parent, f.retPtr, f.own = nil, nil, nil, nil, nil, Contract{}
		f.stack.data = f.stack.data[:0]
		if cap(f.memory.store) > asmMemKeep {
			f.memory.store = make([]byte, 0, asmMemInit)
		}
		f.memory.store = f.memory.store[:0]
		// f.mem is GC-visible: refresh it so a replaced store is not kept alive.
		f.mem, f.memCap = unsafe.Pointer(unsafe.SliceData(f.memory.store)), uint64(cap(f.memory.store))
		in.evm.state.frames = append(in.evm.state.frames, f)
	}
	in.frames = in.frames[:0]
}

func (in *Interpreter) runAsm(contract *Contract) ([]byte, error) {
	if hostStats != nil {
		hostStats[256]++
	}
	depth := in.evm.depth
	f := in.frameAt(depth)
	st := in.evm.state
	f.parent, f.lazy, f.depth, f.static = nil, 0, uint64(depth), 0
	if in.readOnly {
		f.static = 1
	}
	f.contract = contract
	f.scope = scopeContext{Memory: &f.memory, Stack: &f.stack, Contract: contract}
	f.stack.data = f.stack.data[:0]
	f.memory.store = f.memory.store[:0]
	f.memory.lastGasCost = 0
	if contract.dec == nil {
		contract.dec = st.decodedFor(contract.codeAddr, contract.Code)
	}
	f.ctab = nil
	if archNativeCalls && nativeCalls && in.asmFlags&asmFlagCall2929 != 0 && !in.evm.useAOT && depth <= MaxCallDepth {
		in.frameAt(depth + 1) // f.next
		st.noteCode(contract)
		f.ctab, f.journal = st.ctab, &st.journal
	}
	f.handlers = unsafe.Pointer(unsafe.SliceData(contract.dec.handlers))
	f.args = unsafe.Pointer(unsafe.SliceData(contract.dec.args))
	f.code = unsafe.Pointer(unsafe.SliceData(contract.Code))
	f.codeLen = uint64(len(contract.Code))
	f.input = unsafe.Pointer(unsafe.SliceData(contract.Input))
	f.inputLen = uint64(len(contract.Input))
	f.flags = in.asmFlags
	f.env[0].SetBytes(contract.self[:])
	f.env[1].SetBytes(contract.caller[:])
	f.env[2] = contract.value
	f.time = in.evm.Block.Time
	if keccakCacheOn {
		f.kcache = &in.kcache
		if keccakMemoOn {
			in.kcache.tab, in.kcache.shift = st.keccakMemo(), 64-12 // log2(keccakMemoSize)
		}
	}
	if nativeSload && in.asmFlags&asmFlagSload2929 != 0 {
		f.stab = st.slotTable()
		f.nj = nil
		if nativeSstore && in.asmFlags&asmFlagSstore2929 != 0 {
			f.nj = st.nativeJournal()
		}
		f.self = wordsOf(&contract.self)
		f.selfHash = f.self.hash()
	}
	f.abort = &in.evm.abort
	f.pc, f.gas, f.sp, f.status = 0, contract.Gas, 0, 0
	f.syncFromGo()

	roIn, roEVM := in.readOnly, in.evm.readOnly
	in.asmNest++
	if hostNanos != nil {
		t0 := time.Now()
		asmRun(f)
		childNanos += int64(time.Since(t0))
	} else {
		asmRun(f)
	}
	in.asmNest--
	st.flushNative()
	// Host callbacks of natively entered frames set these for their frame.
	in.evm.depth, in.readOnly, in.evm.readOnly = depth, roIn, roEVM

	contract.Gas = f.gas
	ret, err := f.result()
	if in.asmNest == 0 {
		in.releaseFrames()
	}
	return ret, err
}

// result is the output and error of the frame that exited.
func (f *asmFrame) result() (ret []byte, err error) {
	switch f.status {
	case asmStop:
	case asmReturn, asmRevert:
		if f.ret1 != 0 {
			ret = bytes.Clone(unsafe.Slice((*byte)(f.mem), f.memLen)[f.ret0 : f.ret0+f.ret1])
		}
		if f.status == asmRevert {
			f.in.returnData = ret
			err = ErrExecutionReverted
		}
	case asmInvalid:
		err = ErrInvalidOpcode
	case asmHostExit:
		ret, err = bytes.Clone(f.res), f.err
		if err == errStopToken {
			err = nil
		} else if err == ErrExecutionReverted {
			f.in.returnData = ret
		}
	default:
		panic(fmt.Sprintf("asmevm: bad exit status %d pc=%d gas=%d sp=%d", f.status, f.pc, f.gas, f.sp))
	}
	f.res, f.err = nil, nil
	return ret, err
}

// adopt builds the Go-side Contract of a natively entered frame, on its first
// host callback.
func (f *asmFrame) adopt() {
	f.lazy = 0
	codeAddr := Address(f.codeWord.Bytes20())
	code, jd := f.in.evm.state.codeAndJumpdests(codeAddr)
	f.own = Contract{caller: Address(f.env[1].Bytes20()), self: Address(f.env[0].Bytes20()), codeAddr: codeAddr,
		value: f.env[2], Code: code, Input: unsafe.Slice((*byte)(f.input), f.inputLen), Gas: f.gas, analysis: jd,
		dec: f.in.evm.state.accounts[codeAddr].dec}
	f.contract = &f.own
	f.scope = scopeContext{Memory: &f.memory, Stack: &f.stack, Contract: f.contract}
}

// asmChildExit finishes a natively entered frame that did not end in a plain
// STOP or RETURN (those return to the caller in assembly): REVERT, INVALID or
// any exit through the host. It does what EVM.Call and opCall do after Run:
// revert to the snapshot on error, the gas back, the 0/1 result, the return
// data copy and returnData, and moves the caller past its CALL.
func asmChildExit(c *asmFrame) {
	in, p := c.in, c.parent
	in.evm.state.flushNative()
	ret, err := c.result()
	gas := c.gas
	if err != nil {
		in.evm.state.revert(int(c.snap))
		if err != ErrExecutionReverted {
			gas = 0
		}
	}
	p.stack.data = p.stack.data[:p.sp]
	pushCallResult(&p.stack, err)
	p.sp++
	if err == nil || err == ErrExecutionReverted {
		n := minU64(uint64(len(ret)), p.callRetSize)
		copy(unsafe.Slice((*byte)(p.mem), p.memLen)[p.callRetOff:], ret[:n])
	}
	p.gas += gas
	in.returnData = ret
	p.retPtr, p.retLen = unsafe.Pointer(unsafe.SliceData(ret)), uint64(len(ret))
	p.pc++
}

// syncFromGo refreshes the fields Go code may have changed.
func (f *asmFrame) syncFromGo() {
	f.mem = unsafe.Pointer(unsafe.SliceData(f.memory.store))
	f.memLen = uint64(len(f.memory.store))
	f.memCap = uint64(cap(f.memory.store))
	f.memCost = f.memory.lastGasCost
	f.jumpdests = unsafe.Pointer(unsafe.SliceData(f.contract.analysis))
	f.retPtr, f.retLen = unsafe.Pointer(unsafe.SliceData(f.in.returnData)), uint64(len(f.in.returnData))
}

// hostHook, when non-nil, runs at the start of every host callback (tests:
// forced GC inside a natively entered frame).
var hostHook func(f *asmFrame)

// asmHost runs the operation at f.pc through the Go per-op path. asmRun calls it
// with ABI0 for every op, or failing check, it does not handle natively.
func asmHost(f *asmFrame) {
	f.in.evm.state.flushNative()
	if hostHook != nil {
		hostHook(f)
	}
	if f.lazy != 0 {
		f.adopt()
	}
	// The frame may have been entered natively: put the interpreter in its context.
	in := f.in
	in.evm.depth, in.readOnly, in.evm.readOnly = int(f.depth), f.static != 0, f.static != 0
	in.returnData = unsafe.Slice((*byte)(f.retPtr), f.retLen)
	contract := f.contract
	pc0 := f.pc
	var t0 time.Time
	timed := hostNanos != nil && !nestsFrames(contract.Code[f.pc])
	call := hostNanos != nil && !timed
	if timed || call {
		t0 = time.Now()
	}
	if call {
		saved := childNanos
		childNanos = 0
		defer func() {
			callNanos += int64(time.Since(t0)) - childNanos
			callTimed++
			childNanos = saved
		}()
	}
	if hostStats != nil {
		hostStats[contract.Code[f.pc]]++
	}
	contract.Gas = f.gas
	f.stack.data = f.stack.data[:f.sp]
	f.memory.store = f.memory.store[:f.memLen]
	f.memory.lastGasCost = f.memCost

	// SLOAD: accessSlot caches a cold slot; after a revert emptied the table
	// (stab.cold 0) a warm slot is cached here.
	var key uint256.Int
	sload := f.stab != nil && f.stab.cold == 0 && contract.Code[f.pc] == byte(SLOAD) && f.sp > 0
	if sload {
		key = f.stack.data[f.sp-1]
	}

	res, err := f.in.asmStep(&f.pc, &f.scope)
	if sload && err == nil {
		orig := in.evm.state.committedStorage(contract.self, Hash(key.Bytes32()))
		f.stab.put(&f.self, &key, &f.stack.data[f.sp-1], &orig)
	}

	f.pc++
	f.gas, f.sp = contract.Gas, uint64(len(f.stack.data))
	f.syncFromGo()
	if err != nil {
		f.res, f.err, f.status = res, err, asmHostExit
	}
	if timed {
		dt := int64(time.Since(t0))
		*hostNanos += dt
		HostOpNanos[contract.Code[pc0]] += dt
		hostTimed++
	}
}

func (in *Interpreter) asmStep(pc *uint64, scope *scopeContext) ([]byte, error) {
	contract := scope.Contract
	stack := scope.Stack
	op := OpCode(contract.Code[*pc])
	operation := &in.table[op]
	if operation.undefined {
		return nil, errInvalidOp(op)
	}
	sLen := stack.len()
	if sLen < operation.minStack {
		return nil, errUnderflow(sLen, operation.minStack)
	} else if sLen > operation.maxStack {
		return nil, errOverflow(sLen, operation.maxStack)
	}
	if !contract.useGas(operation.constantGas) {
		return nil, ErrOutOfGas
	}
	if operation.dynamicGas != nil {
		var memorySize uint64
		if operation.memorySize != nil {
			memSize, overflow := operation.memorySize(stack)
			if overflow {
				return nil, ErrGasUintOverflow
			}
			if memorySize, overflow = safeMul(toWordSize(memSize), 32); overflow {
				return nil, ErrGasUintOverflow
			}
		}
		dyn, err := operation.dynamicGas(in.evm, contract, stack, scope.Memory, memorySize)
		if err != nil || !contract.useGas(dyn) { // geth: any dynamic gas error is out of gas
			return nil, ErrOutOfGas
		}
		if memorySize > 0 {
			scope.Memory.resize(memorySize)
		}
	}
	return operation.execute(pc, in, scope)
}

func asmYield() { runtime.Gosched() }

func init() {
	if envForceAsm() {
		asmEVMForced = true
	}
}
