package asmevm

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"unsafe"

	"github.com/holiman/uint256"
)

// AOT adapter. aotFunction is the exact ABI0 entry shape that cmd/evmaot
// (worktrees/evm-aot-*, the Codex tab's compiler) emits for amd64, so its
// generated files plug in mechanically: change `package vm` to
// `package asmevm`; their init calls registerAOT. With EnableAOT, the Go
// loop (runGo) enters a registered program at every pc it marks as an entry
// and runs the op the program stops at itself, as the libevm integration does
// (core/vm/interpreter.go in those worktrees). The interpreter is the fallback
// for every op the program does not compile.
//
// Contract of one call (as the generator defines it):
//   - stack/stackLen/stackCap: the frame's []uint256.Int (little-endian limbs),
//     item i at stack[i]; newLen is the length after the call.
//   - pc: the entry pc; ^0 means "initialise": the program writes 1 into
//     entries[pc] for every pc it can enter and destinations[pc] for every
//     JUMPDEST, and returns.
//   - gas in, newGas out. abort is EVM.Cancel's flag.
//   - memory/memoryLen, calldata/calldataLen: read-only views; the program
//     never grows memory (it stops before an op that needs expansion).
//   - newPC is the first op it did not execute, with no state change for that
//     op; executed counts the ops it ran (0 when it could not run the entry).
type aotFunction func(stack *uint256.Int, stackLen, stackCap, pc, gas uint64, abort *atomic.Bool, entries, destinations *uintptr, memory *byte, memoryLen uint64, calldata *byte, calldataLen uint64) (newLen, newPC, newGas, executed uint64)

type aotProgram struct {
	code                  []byte
	run                   aotFunction
	entries, destinations []uintptr
}

var (
	aotPrograms = map[Hash]*aotProgram{}
	aotByCode   = map[*byte]*aotProgram{} // lookup cache by code slice; nil = none
)

// registerAOT is the registration call cmd/evmaot emits.
func registerAOT(codeHex string, run aotFunction) {
	code, err := hex.DecodeString(codeHex)
	if err != nil {
		panic(err)
	}
	if len(code) == 0 {
		return
	}
	p := &aotProgram{code: code, run: run, entries: make([]uintptr, len(code)), destinations: make([]uintptr, len(code))}
	var abort atomic.Bool
	run(nil, 0, 0, ^uint64(0), 0, &abort, &p.entries[0], &p.destinations[0], nil, 0, nil, 0)
	aotPrograms[Keccak256(code)] = p
	clear(aotByCode)
}

// findAOT returns the program compiled from exactly this code, or nil.
func findAOT(code []byte) *aotProgram {
	if len(aotPrograms) == 0 || len(code) == 0 {
		return nil
	}
	if p, ok := aotByCode[&code[0]]; ok && (p == nil || len(p.code) == len(code)) {
		return p
	}
	p := aotPrograms[Keccak256(code)]
	if p != nil && !bytes.Equal(p.code, code) {
		p = nil
	}
	aotByCode[&code[0]] = p
	return p
}

// EnableAOT runs contracts that have a registered AOT program through it,
// with the Go interpreter as the fallback (single-goroutine use, like EVM).
func (evm *EVM) EnableAOT(on bool) { evm.useAOT = on }

// enter runs the program from pc and returns the pc of the op it stopped at.
func (p *aotProgram) enter(contract *Contract, stack *Stack, mem *Memory, pc uint64, abort *atomic.Bool) uint64 {
	var memPtr, cdPtr *byte
	if len(mem.store) != 0 {
		memPtr = &mem.store[0]
	}
	if len(contract.Input) != 0 {
		cdPtr = &contract.Input[0]
	}
	n, next, gas, _ := p.run(&stack.data[:cap(stack.data)][0], uint64(len(stack.data)), uint64(cap(stack.data)), pc, contract.Gas, abort,
		&p.entries[0], &p.destinations[0], memPtr, uint64(len(mem.store)), cdPtr, uint64(len(contract.Input)))
	stack.data = stack.data[:n]
	contract.Gas = gas
	return next
}

// Native AOT (package aot, amd64): compiled code that runs inside the core
// with the core's registers (ABI.md section 15). The hook replaces
// handlers[pc] at the compiled entry pcs of a registered code when the stream
// is built, so the core enters compiled code by its normal dispatch.
var (
	nativeAOT   = map[int][]nativeProgram{} // by code length
	nativeAOTOn bool
)

type nativeProgram struct {
	code    []byte
	entries [][2]uint64 // pc, address
}

// RegisterNativeAOT attaches compiled entries ({pc, address}) to code.
func RegisterNativeAOT(code []byte, entries [][2]uint64) {
	if len(nativeAOT) == 0 {
		base := predecodeHook
		predecodeHook = func(code []byte) *decoded {
			d := base(code)
			if nativeAOTOn {
				for _, p := range nativeAOT[len(code)] {
					if bytes.Equal(p.code, code) {
						for _, e := range p.entries {
							d.handlers[e[0]] = e[1]
						}
						break
					}
				}
			}
			return d
		}
	}
	nativeAOT[len(code)] = append(nativeAOT[len(code)], nativeProgram{code, entries})
}

// SetNativeAOT switches the hook for streams built from now on (state set up
// later; streams already cached on accounts keep what they have).
func SetNativeAOT(on bool) { nativeAOTOn = on }

// NativeSlots returns the dispatch slot addresses (opcode handlers); zero
// when the core is not built in.
func NativeSlots() (s [256]uint64) { copy(s[:], slotAddr[:256]); return }

// NativeFrameLayout names the asmFrame offsets compiled code uses.
func NativeFrameLayout() string {
	var f asmFrame
	return fmt.Sprintf("mem=%d memLen=%d memCap=%d memCost=%d input=%d inputLen=%d env=%d",
		unsafe.Offsetof(f.mem), unsafe.Offsetof(f.memLen), unsafe.Offsetof(f.memCap), unsafe.Offsetof(f.memCost),
		unsafe.Offsetof(f.input), unsafe.Offsetof(f.inputLen), unsafe.Offsetof(f.env))
}

// AssumePredicatesValid lets a profiling replay (AOT block profile on a
// training window) run blocks whose header is not in the sidecar. Never for a gate.
func AssumePredicatesValid(on bool) { ccAssumePredicatesValid = on }
