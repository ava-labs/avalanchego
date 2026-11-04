package asmevm

import (
	"reflect"
	"sync/atomic"
	"unsafe"

	"github.com/holiman/uint256"
)

// asmFrame is the per-frame state shared with core_arm64.s. The first 17 fields
// (code..ret1) must keep this exact order and offsets; the assembly reads the
// pointer fields and writes the integer fields. Go-only fields follow.
type asmFrame struct {
	code      unsafe.Pointer // 0
	codeLen   uint64         // 8
	stackBase unsafe.Pointer // 16, 1024 words
	mem       unsafe.Pointer // 24
	memLen    uint64         // 32
	memCap    uint64         // 40
	memCost   uint64         // 48
	jumpdests unsafe.Pointer // 56
	input     unsafe.Pointer // 64
	inputLen  uint64         // 72
	flags     uint64         // 80
	abort     *atomic.Bool   // 88
	pc        uint64         // 96
	gas       uint64         // 104
	sp        uint64         // 112
	status    uint64         // 120
	ret0      uint64         // 128
	ret1      uint64         // 136
	handlers  unsafe.Pointer // pre-decoded stream: handler address per pc
	args      unsafe.Pointer // pre-decoded PUSH immediates per pc
	kstate    [25]uint64     // amd64 KECCAK256 state
	kpad      [17]uint64     // amd64 KECCAK256 padded last block
	env       [3]uint256.Int // amd64 ADDRESS, CALLER, CALLVALUE as stack words
	retLen    uint64         // amd64 RETURNDATASIZE
	time      uint64         // amd64 TIMESTAMP
	kcache    *keccakCache   // amd64 KECCAK256 of 64-byte inputs; nil: off
	stab      *slotTable     // amd64 warm slots; nil: SLOAD in Go
	self      addrWords      // amd64 the storage account (contract.self) for stab
	selfHash  uint64         // amd64 self.hash()
	// amd64 native CALL (see calls in core_amd64.s and asmChildExit).
	ctab        *codeTable     // accounts a native CALL may enter; nil: CALL in Go
	nj          *nativeJournal // native SSTORE records; nil: SSTORE in Go
	journal     *[]jentry      // &State.journal: its length is the snapshot
	next        *asmFrame      // record of the next depth, the native callee
	parent      *asmFrame      // native caller; nil for a frame entered from Go
	depth       uint64         // EVM depth of this frame
	static      uint64         // 1 in a static context
	snap        uint64         // journal length at the native call
	lazy        uint64         // 1: entered natively, Go-side Contract not built yet
	callRetOff  uint64         // the return region of the native call in flight
	callRetSize uint64
	retPtr      unsafe.Pointer // return data of the last call (retLen bytes)
	codeWord    uint256.Int    // code address of a natively entered frame
	own         Contract       // the Contract of a natively entered frame

	in       *Interpreter
	contract *Contract
	stack    Stack
	memory   Memory
	res      []byte
	err      error
	scope    scopeContext // built once per frame so host callbacks do not allocate
}

// keccakCache holds the last 8 KECCAK256 results of 64-byte inputs (mapping
// slots hash key . slot, and contracts rehash the same slots), per
// interpreter. Exact input match, so a hit is always the right result.
type keccakCache struct {
	next uint64 // entry replaced next
	n    uint64 // entries in use, 0..8
	e    [8]keccakEntry
	// tab, when set, replaces e: a direct-mapped memo of keccakMemoSize
	// entries owned by the State (kept across transactions), indexed by
	// (xor of the 8 input words * slotHashMul) >> shift. Unused entries hold
	// the 64 zero bytes and their hash, so every entry is a correct result.
	tab   unsafe.Pointer
	shift uint64
}

type keccakEntry struct {
	in  [8]uint64   // the input bytes
	out uint256.Int // the result as a stack word
}

const keccakMemoSize = 4096

// keccakMemo returns the State's KECCAK256 memo (see keccakCache.tab), making it.
func (s *State) keccakMemo() unsafe.Pointer {
	if s.kmemo == nil {
		h := Keccak256(make([]byte, 64))
		z := keccakEntry{out: *new(uint256.Int).SetBytes32(h[:])}
		s.kmemo = new([keccakMemoSize]keccakEntry)
		for i := range s.kmemo {
			s.kmemo[i] = z
		}
	}
	return unsafe.Pointer(s.kmemo)
}

var _ [96]byte = [unsafe.Sizeof(keccakCache{}.e[0])]byte{} // core_amd64.s steps by 96

// decoded is the pre-decoded instruction stream of one code, indexed by pc:
// handlers[pc] is the address of the native handler for the instruction at pc
// (PUSH data bytes get the INVALID handler, 33 STOP entries pad the end so a
// PUSH32 can never run off), args[pc] the PUSH1..8 value or a pointer to the
// 256-bit PUSH9..32 value in consts.
type decoded struct {
	handlers []uint64
	args     []uint64
	consts   []uint256.Int
	consts2  []uint256.Int // folded constants of superinstructions
}

// jumpdestHandler is the JUMPDEST slot address (amd64 jump validation).
var jumpdestHandler uint64

// slotAddr[k] is the address of dispatch slot k (amd64 dispatch hints).
var slotAddr [256 + 32]uint64 // 256 + at least fusedCount (checked in asm_core.go)

// predecodeHook builds the stream; nil when the asm core is not built in.
var predecodeHook func(code []byte) *decoded

// Exit codes in asmFrame.status. Zero means running.
const (
	asmStop = iota + 1
	asmReturn
	asmRevert
	asmInvalid
	asmHostExit
	asmSlotCheck = 0x5107
)

// Fork-dependent native opcodes; the others are the same in every fork.
const (
	asmFlagShifts = 1 << iota
	asmFlagPush0
	asmFlagMcopy
	asmFlagRevert
	asmFlagExpEIP158
	asmFlagReturnData
	asmFlagSload2929
	asmFlagCall2929
	asmFlagSstore2929
)

const (
	asmMemInit = 4 << 10
	asmMemKeep = 1 << 20
)

// asmFlagsFor reports which fork-dependent native opcodes the table enables.
func asmFlagsFor(table *[256]operation) uint64 {
	undefined := reflect.ValueOf(opUndefined).Pointer()
	defined := func(op OpCode) bool {
		return reflect.ValueOf(table[op].execute).Pointer() != undefined
	}
	var flags uint64
	if defined(SHL) {
		flags |= asmFlagShifts
	}
	if defined(PUSH0) {
		flags |= asmFlagPush0
	}
	if defined(MCOPY) {
		flags |= asmFlagMcopy
	}
	if defined(REVERT) {
		flags |= asmFlagRevert
	}
	if defined(RETURNDATASIZE) {
		flags |= asmFlagReturnData
	}
	if reflect.ValueOf(table[SLOAD].dynamicGas).Pointer() == reflect.ValueOf(gasSLoadEIP2929).Pointer() {
		flags |= asmFlagSload2929
	}
	if reflect.ValueOf(table[SSTORE].dynamicGas).Pointer() == reflect.ValueOf(gasSStoreEIP2929).Pointer() {
		flags |= asmFlagSstore2929
	}
	if reflect.ValueOf(table[CALL].dynamicGas).Pointer() == reflect.ValueOf(gasCallEIP2929).Pointer() &&
		reflect.ValueOf(table[STATICCALL].dynamicGas).Pointer() == reflect.ValueOf(gasDelegateOrStaticEIP2929).Pointer() &&
		reflect.ValueOf(table[DELEGATECALL].dynamicGas).Pointer() == reflect.ValueOf(gasDelegateOrStaticEIP2929).Pointer() {
		flags |= asmFlagCall2929
	}
	if reflect.ValueOf(table[EXP].dynamicGas).Pointer() == reflect.ValueOf(gasExpEIP158).Pointer() {
		flags |= asmFlagExpEIP158
	}
	return flags
}

// AsmAvailable reports whether the native core is built in and the CPU supports it.
func AsmAvailable() bool { return asmEVMAvailable }
