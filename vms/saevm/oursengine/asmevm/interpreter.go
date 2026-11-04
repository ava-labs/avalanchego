package asmevm

import "github.com/holiman/uint256"

type scopeContext struct {
	Memory   *Memory
	Stack    *Stack
	Contract *Contract
}

type (
	executionFunc  func(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error)
	gasFunc        func(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error)
	memorySizeFunc func(stack *Stack) (size uint64, overflow bool)
)

type operation struct {
	execute     executionFunc
	constantGas uint64
	dynamicGas  gasFunc
	minStack    int
	maxStack    int
	memorySize  memorySizeFunc
	undefined   bool
}

// Interpreter runs one EVM. The Go loop is the reference; the asm core (opt-in)
// calls back into the Go per-op path for anything it does not handle natively.
type Interpreter struct {
	evm        *EVM
	table      *[256]operation
	readOnly   bool
	returnData []byte
	asmFlags   uint64
	hasher     bool // placeholder for interface parity
	kcache     keccakCache
	frames     []*asmFrame // asm frame records by depth (native CALL switches to frames[d+1])
	asmNest    int         // runAsm calls in progress; the outermost releases frames
}

func newInterpreter(evm *EVM) *Interpreter {
	in := &Interpreter{evm: evm, table: tableFor(evm.Block.Rules)}
	in.asmFlags = asmFlagsFor(in.table)
	return in
}

// Run executes contract and returns its output. input is the calldata.
func (in *Interpreter) Run(contract *Contract, input []byte, readOnly bool) (ret []byte, err error) {
	in.evm.depth++
	defer func() { in.evm.depth-- }()

	if readOnly && !in.readOnly {
		in.readOnly = true
		defer func() { in.readOnly = false }()
	}
	in.returnData = nil
	if len(contract.Code) == 0 {
		return nil, nil
	}
	contract.Input = input
	if contract.analysis == nil {
		contract.analysis = codeBitmap(contract.Code)
	}

	if in.evm.coldSelf > 0 && !in.evm.state.warmAddr.has(&contract.self, addrHash(&contract.self)) {
		// NativeAssetCall's callee (or a DELEGATECALL of it) before a slot access warms it
		// (State.accessSlot). The asm core and the AOT programs warm a cold slot natively
		// without its address, so the Go loop runs this frame.
		return in.runGo(contract, nil)
	}
	if in.evm.useAOT {
		if p := findAOT(contract.Code); p != nil {
			return in.runGo(contract, p)
		}
	}
	if in.evm.useAsm && asmEVMAvailable {
		return in.runAsm(contract)
	}
	return in.runGo(contract, nil)
}

// runGo is the reference interpreter loop. With aot non-nil it enters the
// compiled program wherever it has an entry and runs the op it stops at.
func (in *Interpreter) runGo(contract *Contract, aot *aotProgram) (ret []byte, err error) {
	var (
		mem   = &Memory{}
		stack = &Stack{data: make([]uint256.Int, 0, 1024)}
		scope = &scopeContext{Memory: mem, Stack: stack, Contract: contract}
		pc    = uint64(0)
		res   []byte
	)
	for {
		if aot != nil && pc < uint64(len(aot.entries)) && aot.entries[pc] != 0 {
			pc = aot.enter(contract, stack, mem, pc, &in.evm.abort)
		}
		op := STOP // running past the end of code is an implicit STOP
		if pc < uint64(len(contract.Code)) {
			op = OpCode(contract.Code[pc])
		}
		operation := &in.table[op]
		if traceOp != nil {
			traceOp(contract, pc, op)
		}
		if rwOn && in.evm.state.OpHook != nil {
			in.evm.state.OpHook(op, stack)
		}
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
		var memorySize uint64
		if operation.dynamicGas != nil {
			if operation.memorySize != nil {
				memSize, overflow := operation.memorySize(stack)
				if overflow {
					return nil, ErrGasUintOverflow
				}
				if memorySize, overflow = safeMul(toWordSize(memSize), 32); overflow {
					return nil, ErrGasUintOverflow
				}
			}
			dyn, err := operation.dynamicGas(in.evm, contract, stack, mem, memorySize)
			if err != nil || !contract.useGas(dyn) { // geth: any dynamic gas error is out of gas
				return nil, ErrOutOfGas
			}
			if memorySize > 0 {
				mem.resize(memorySize)
			}
		}
		res, err = operation.execute(&pc, in, scope)
		if err != nil {
			break
		}
		pc++
	}
	if err == errStopToken {
		err = nil
	}
	return res, err
}

// traceKeccak, when set, sees every KECCAK256 input of the Go loop (diagnosis only).
var traceKeccak func(in *Interpreter, data []byte)

// traceOp, when set, sees every op the Go loop executes (diagnosis only).
var traceOp func(c *Contract, pc uint64, op OpCode)

func toWordSize(size uint64) uint64 {
	if size > ^uint64(0)-31 {
		return ^uint64(0)/32 + 1
	}
	return (size + 31) / 32
}

func safeMul(a, b uint64) (uint64, bool) {
	if a == 0 || b == 0 {
		return 0, false
	}
	c := a * b
	if c/a != b {
		return 0, true
	}
	return c, false
}

func safeAdd(a, b uint64) (uint64, bool) {
	c := a + b
	return c, c < a
}

var _ = uint256.Int{}
