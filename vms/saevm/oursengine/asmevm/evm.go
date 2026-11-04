package asmevm

import (
	"sync/atomic"

	"github.com/holiman/uint256"
)

// BlockContext holds the block-scoped environment values the EVM reads.
type BlockContext struct {
	Coinbase    Address
	GasLimit    uint64
	BlockNumber uint64
	Time        uint64
	Difficulty  uint256.Int // PREVRANDAO in Cancun
	BaseFee     uint256.Int
	BlobBaseFee uint256.Int
	Random      Hash // PREVRANDAO value
	BlockHashes map[uint64]Hash
	ChainID     uint64         // 0 means 1
	Avalanche   *Avalanche     // nil: Ethereum rules
	Rules       *Rules         // nil: today's rules (Cancun + Granite); else the block's fork (forks.go)
	OnHashMiss  func(n uint64) // an in-range BLOCKHASH absent from BlockHashes (fixture mode panics; eth_call adds it)
}

// TxContext holds the transaction-scoped environment values.
type TxContext struct {
	Origin     Address
	GasPrice   uint256.Int
	BlobHashes []Hash
	WarpFailed []byte // Avalanche: the header's failed-predicate bitset for this tx
}

// EVM is the execution environment for one transaction.
type EVM struct {
	Block   BlockContext
	Tx      TxContext
	state   *State
	chainID uint256.Int

	interp *Interpreter

	depth      int
	readOnly   bool
	returnData []byte
	warpPreds  [][]Hash // Avalanche: this tx's warp predicates (access-list chunks)

	useAsm      bool
	useAOT      bool // registered AOT programs, Go loop as fallback (aot.go)
	coldSelf    int  // NativeAssetCall frames in progress: their callee runs before it is warm
	callGasTemp uint64
	abort       atomic.Bool
}

func NewEVM(block BlockContext, tx TxContext, state *State) *EVM {
	evm := &EVM{Block: block, Tx: tx, state: state}
	evm.chainID.SetUint64(1)
	if block.ChainID != 0 {
		evm.chainID.SetUint64(block.ChainID)
	}
	evm.useAsm = asmEVMForced && asmEVMAvailable
	evm.interp = newInterpreter(evm)
	return evm
}

// EnableAsm turns on the assembly interpreter core (arm64/amd64 with required
// CPU features). It is opt-in; the Go loop stays the reference.
func (evm *EVM) EnableAsm(on bool) { evm.useAsm = on && asmEVMAvailable }

func (evm *EVM) State() *State { return evm.state }

// transfer moves value from sender to recipient (journaled).
func (evm *EVM) transfer(sender, recipient Address, value *uint256.Int) {
	if value.IsZero() {
		return
	}
	evm.state.SubBalance(sender, value)
	evm.state.AddBalance(recipient, value)
}

// canTransfer reports whether addr holds at least value.
func (evm *EVM) canTransfer(addr Address, value *uint256.Int) bool {
	bal := evm.state.Balance(addr)
	return bal.Cmp(value) >= 0
}

// Call runs addr's code with the given input and value transfer.
func (evm *EVM) Call(caller, addr Address, input []byte, gas uint64, value *uint256.Int) ([]byte, uint64, error) {
	if evm.depth > MaxCallDepth {
		return nil, gas, ErrDepth
	}
	if !value.IsZero() && !evm.canTransfer(caller, value) {
		return nil, gas, ErrInsufficientBalance
	}
	snap := evm.state.snapshot()
	p, isPrecompile := evm.lookupPrecompile(addr)

	if !evm.state.Exists(addr) {
		if !isPrecompile && value.IsZero() {
			return nil, gas, nil
		}
		evm.state.getOrNew(addr)
	}
	evm.transfer(caller, addr, value)

	var (
		ret []byte
		err error
	)
	if isPrecompile {
		ret, gas, err = evm.runPrecompileAt(p, caller, input, gas, kindCall)
	} else {
		code, jd := evm.state.codeAndJumpdests(addr)
		if len(code) == 0 {
			return nil, gas, nil
		}
		c := &Contract{caller: caller, self: addr, codeAddr: addr, Code: code, Gas: gas, analysis: jd}
		c.value.Set(value)
		ret, err = evm.interp.Run(c, input, evm.readOnly)
		gas = c.Gas
	}
	if err != nil {
		evm.state.revert(snap)
		if err != ErrExecutionReverted {
			gas = 0
		}
	}
	return ret, gas, err
}

// CallCode runs addr's code in caller's own context (storage of caller).
func (evm *EVM) CallCode(caller, addr Address, input []byte, gas uint64, value *uint256.Int) ([]byte, uint64, error) {
	if evm.depth > MaxCallDepth {
		return nil, gas, ErrDepth
	}
	if !value.IsZero() && !evm.canTransfer(caller, value) {
		return nil, gas, ErrInsufficientBalance
	}
	snap := evm.state.snapshot()
	p, isPrecompile := evm.lookupPrecompile(addr)
	var (
		ret []byte
		err error
	)
	if isPrecompile {
		ret, gas, err = evm.runPrecompileAt(p, caller, input, gas, kindCallCode)
	} else {
		code, jd := evm.state.codeAndJumpdests(addr)
		c := &Contract{caller: caller, self: caller, codeAddr: addr, Code: code, Gas: gas, analysis: jd}
		c.value.Set(value)
		ret, err = evm.interp.Run(c, input, evm.readOnly)
		gas = c.Gas
	}
	if err != nil {
		evm.state.revert(snap)
		if err != ErrExecutionReverted {
			gas = 0
		}
	}
	return ret, gas, err
}

// DelegateCall runs addr's code with caller's self, caller and value preserved.
func (evm *EVM) DelegateCall(caller, self Address, callerVal *uint256.Int, origCaller Address, addr Address, input []byte, gas uint64) ([]byte, uint64, error) {
	if evm.depth > MaxCallDepth {
		return nil, gas, ErrDepth
	}
	snap := evm.state.snapshot()
	p, isPrecompile := evm.lookupPrecompile(addr)
	var (
		ret []byte
		err error
	)
	if isPrecompile {
		ret, gas, err = evm.runPrecompileAt(p, caller, input, gas, kindDelegate)
	} else {
		code, jd := evm.state.codeAndJumpdests(addr)
		c := &Contract{caller: origCaller, self: self, codeAddr: addr, Code: code, Gas: gas, analysis: jd}
		c.value.Set(callerVal)
		ret, err = evm.interp.Run(c, input, evm.readOnly)
		gas = c.Gas
	}
	if err != nil {
		evm.state.revert(snap)
		if err != ErrExecutionReverted {
			gas = 0
		}
	}
	return ret, gas, err
}

// StaticCall runs addr's code read-only.
func (evm *EVM) StaticCall(caller, addr Address, input []byte, gas uint64) ([]byte, uint64, error) {
	if evm.depth > MaxCallDepth {
		return nil, gas, ErrDepth
	}
	snap := evm.state.snapshot()
	p, isPrecompile := evm.lookupPrecompile(addr)
	var (
		ret []byte
		err error
	)
	if isPrecompile {
		ret, gas, err = evm.runPrecompileAt(p, caller, input, gas, kindStatic)
	} else {
		code, jd := evm.state.codeAndJumpdests(addr)
		if len(code) == 0 {
			return nil, gas, nil
		}
		c := &Contract{caller: caller, self: addr, codeAddr: addr, Code: code, Gas: gas, analysis: jd}
		prev := evm.readOnly
		evm.readOnly = true
		ret, err = evm.interp.Run(c, input, true)
		evm.readOnly = prev
		gas = c.Gas
	}
	if err != nil {
		evm.state.revert(snap)
		if err != ErrExecutionReverted {
			gas = 0
		}
	}
	return ret, gas, err
}

// create is the shared CREATE/CREATE2 implementation.
func (evm *EVM) create(caller Address, code []byte, gas uint64, value *uint256.Int, addr Address) ([]byte, Address, uint64, error) {
	if evm.depth > MaxCallDepth {
		return nil, Address{}, gas, ErrDepth
	}
	if !evm.canTransfer(caller, value) {
		return nil, Address{}, gas, ErrInsufficientBalance
	}
	nonce := evm.state.Nonce(caller)
	if nonce+1 < nonce {
		return nil, Address{}, gas, ErrNonceUintOverflow
	}
	evm.state.SetNonce(caller, nonce+1)
	evm.state.accessAddr(addr)

	if evm.state.Nonce(addr) != 0 || evm.state.CodeSize(addr) != 0 || evm.state.hasStorage(addr) {
		return nil, Address{}, 0, ErrContractAddressCollision
	}
	snap := evm.state.snapshot()
	evm.state.getOrNew(addr)
	evm.state.SetNonce(addr, 1)
	evm.state.markCreated(addr)
	evm.transfer(caller, addr, value)

	c := &Contract{caller: caller, self: addr, codeAddr: addr, Code: code, Gas: gas}
	c.value.Set(value)
	ret, err := evm.interp.Run(c, nil, false)
	gas = c.Gas

	if err == nil && len(ret) > MaxCodeSize {
		err = ErrMaxCodeSizeExceeded
	}
	if err == nil && len(ret) >= 1 && ret[0] == 0xef && evm.Block.Rules.london() { // EIP-3541
		err = ErrInvalidCode
	}
	if err == nil {
		createDataGas := uint64(len(ret)) * CreateDataGas
		if gas < createDataGas {
			err = ErrCodeStoreOutOfGas
		} else {
			gas -= createDataGas
			evm.state.SetCode(addr, ret)
		}
	}
	if err != nil {
		evm.state.revert(snap)
		if err != ErrExecutionReverted {
			gas = 0
		}
	}
	return ret, addr, gas, err
}

func (evm *EVM) Create(caller Address, code []byte, gas uint64, value *uint256.Int) ([]byte, Address, uint64, error) {
	addr := createAddress(caller, evm.state.Nonce(caller))
	return evm.create(caller, code, gas, value, addr)
}

func (evm *EVM) Create2(caller Address, code []byte, gas uint64, endowment *uint256.Int, salt *uint256.Int) ([]byte, Address, uint64, error) {
	addr := create2Address(caller, salt, code)
	return evm.create(caller, code, gas, endowment, addr)
}
