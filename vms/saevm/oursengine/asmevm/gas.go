package asmevm

import "github.com/holiman/uint256"

const quadCoeffDiv = 512

// memoryGasCost returns the gas for growing mem to newMemSize bytes.
func memoryGasCost(mem *Memory, newMemSize uint64) (uint64, error) {
	if newMemSize == 0 {
		return 0, nil
	}
	if newMemSize > 0x1FFFFFFFE0 {
		return 0, ErrGasUintOverflow
	}
	newMemSizeWords := toWordSize(newMemSize)
	newMemSize = newMemSizeWords * 32
	if newMemSize <= uint64(mem.len()) {
		return 0, nil
	}
	square := newMemSizeWords * newMemSizeWords
	linCoef := newMemSizeWords * MemoryGas
	quadCoef := square / quadCoeffDiv
	newTotalFee := linCoef + quadCoef
	fee := newTotalFee - mem.lastGasCost
	mem.lastGasCost = newTotalFee
	return fee, nil
}

// --- memory size functions ---

func memoryKeccak256(stack *Stack) (uint64, bool) { return calcMemSize64(stack.back(0), stack.back(1)) }
func memoryMLoad(stack *Stack) (uint64, bool)     { return calcMemSize64WithUint(stack.back(0), 32) }
func memoryMStore(stack *Stack) (uint64, bool)    { return calcMemSize64WithUint(stack.back(0), 32) }
func memoryMStore8(stack *Stack) (uint64, bool)   { return calcMemSize64WithUint(stack.back(0), 1) }
func memoryCallDataCopy(stack *Stack) (uint64, bool) {
	return calcMemSize64(stack.back(0), stack.back(2))
}
func memoryCodeCopy(stack *Stack) (uint64, bool) { return calcMemSize64(stack.back(0), stack.back(2)) }
func memoryExtCodeCopy(stack *Stack) (uint64, bool) {
	return calcMemSize64(stack.back(1), stack.back(3))
}
func memoryReturnDataCopy(stack *Stack) (uint64, bool) {
	return calcMemSize64(stack.back(0), stack.back(2))
}
func memoryReturn(stack *Stack) (uint64, bool) { return calcMemSize64(stack.back(0), stack.back(1)) }
func memoryRevert(stack *Stack) (uint64, bool) { return calcMemSize64(stack.back(0), stack.back(1)) }
func memoryMcopy(stack *Stack) (uint64, bool) {
	a, ao := calcMemSize64(stack.back(0), stack.back(2))
	b, bo := calcMemSize64(stack.back(1), stack.back(2))
	if ao || bo {
		return 0, true
	}
	if a > b {
		return a, false
	}
	return b, false
}
func memoryLog(stack *Stack) (uint64, bool) { return calcMemSize64(stack.back(0), stack.back(1)) }
func memoryCall(stack *Stack) (uint64, bool) {
	x, overflow := calcMemSize64(stack.back(5), stack.back(6))
	if overflow {
		return 0, true
	}
	y, overflow := calcMemSize64(stack.back(3), stack.back(4))
	if overflow {
		return 0, true
	}
	if x > y {
		return x, false
	}
	return y, false
}
func memoryDelegateCall(stack *Stack) (uint64, bool) {
	x, overflow := calcMemSize64(stack.back(4), stack.back(5))
	if overflow {
		return 0, true
	}
	y, overflow := calcMemSize64(stack.back(2), stack.back(3))
	if overflow {
		return 0, true
	}
	if x > y {
		return x, false
	}
	return y, false
}
func memoryCreate(stack *Stack) (uint64, bool)  { return calcMemSize64(stack.back(1), stack.back(2)) }
func memoryCreate2(stack *Stack) (uint64, bool) { return calcMemSize64(stack.back(1), stack.back(2)) }

func calcMemSize64(off, length *uint256.Int) (uint64, bool) {
	if !length.IsUint64() {
		return 0, true
	}
	return calcMemSize64WithUint(off, length.Uint64())
}
func calcMemSize64WithUint(off *uint256.Int, length64 uint64) (uint64, bool) {
	if length64 == 0 {
		return 0, false
	}
	offset64, overflow := off.Uint64WithOverflow()
	if overflow {
		return 0, true
	}
	val := offset64 + length64
	return val, val < offset64
}

// --- dynamic gas functions ---

func gasKeccak256(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	gas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return 0, err
	}
	wordGas, overflow := stack.back(1).Uint64WithOverflow()
	if overflow {
		return 0, ErrGasUintOverflow
	}
	if wordGas, overflow = safeMul(toWordSize(wordGas), Keccak256WordGas); overflow {
		return 0, ErrGasUintOverflow
	}
	if gas, overflow = safeAdd(gas, wordGas); overflow {
		return 0, ErrGasUintOverflow
	}
	return gas, nil
}

func makeGasCopy(stackpos int) gasFunc {
	return func(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
		gas, err := memoryGasCost(mem, memorySize)
		if err != nil {
			return 0, err
		}
		words, overflow := stack.back(stackpos).Uint64WithOverflow()
		if overflow {
			return 0, ErrGasUintOverflow
		}
		if words, overflow = safeMul(toWordSize(words), CopyGas); overflow {
			return 0, ErrGasUintOverflow
		}
		if gas, overflow = safeAdd(gas, words); overflow {
			return 0, ErrGasUintOverflow
		}
		return gas, nil
	}
}

func gasMLoadStore(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	return memoryGasCost(mem, memorySize)
}

func gasExpEIP158(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	expByteLen := uint64((stack.back(1).BitLen() + 7) / 8)
	gas := expByteLen * ExpByteGas
	return gas, nil
}

func gasMcopy(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	gas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return 0, err
	}
	words, overflow := stack.back(2).Uint64WithOverflow()
	if overflow {
		return 0, ErrGasUintOverflow
	}
	if words, overflow = safeMul(toWordSize(words), CopyGas); overflow {
		return 0, ErrGasUintOverflow
	}
	if gas, overflow = safeAdd(gas, words); overflow {
		return 0, ErrGasUintOverflow
	}
	return gas, nil
}

func makeGasLog(n uint64) gasFunc {
	return func(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
		requestedSize, overflow := stack.back(1).Uint64WithOverflow()
		if overflow {
			return 0, ErrGasUintOverflow
		}
		gas, err := memoryGasCost(mem, memorySize)
		if err != nil {
			return 0, err
		}
		if gas, overflow = safeAdd(gas, LogGas); overflow {
			return 0, ErrGasUintOverflow
		}
		if gas, overflow = safeAdd(gas, n*LogTopicGas); overflow {
			return 0, ErrGasUintOverflow
		}
		var dataGas uint64
		if dataGas, overflow = safeMul(requestedSize, LogDataGas); overflow {
			return 0, ErrGasUintOverflow
		}
		if gas, overflow = safeAdd(gas, dataGas); overflow {
			return 0, ErrGasUintOverflow
		}
		return gas, nil
	}
}

// --- EIP-2929 account access ops (BALANCE, EXTCODESIZE, EXTCODEHASH) ---

func gasEip2929AccountCheck(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	addr := Address(stack.back(0).Bytes20())
	if evm.state.accessAddr(addr) {
		return ColdAccountAccessCost, nil
	}
	return WarmStorageReadCost, nil
}

func gasExtCodeCopyEIP2929(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	gas, err := makeGasCopy(3)(evm, c, stack, mem, memorySize)
	if err != nil {
		return 0, err
	}
	addr := Address(stack.back(0).Bytes20())
	var cold uint64 = WarmStorageReadCost
	if evm.state.accessAddr(addr) {
		cold = ColdAccountAccessCost
	}
	var overflow bool
	if gas, overflow = safeAdd(gas, cold); overflow {
		return 0, ErrGasUintOverflow
	}
	return gas, nil
}

// --- SLOAD / SSTORE (EIP-2929 + EIP-2200 + EIP-3529) ---

func gasSLoadEIP2929(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	slot := Hash(stack.back(0).Bytes32())
	if evm.state.accessSlot(c.self, slot) {
		return ColdSloadCost, nil
	}
	return WarmStorageReadCost, nil
}

func gasSStoreEIP2929(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	if c.Gas <= CallStipend {
		return 0, ErrOutOfGas // EIP-2200 sentry
	}
	slot := Hash(stack.back(0).Bytes32())
	valU := stack.back(1)
	var cost uint64
	if evm.state.accessSlot(c.self, slot) {
		cost = ColdSloadCost
	}
	current := evm.state.GetStorage(c.self, slot)
	original := evm.state.committedStorage(c.self, slot)
	value := *valU

	if current.Eq(&value) { // no-op
		return cost + WarmStorageReadCost, nil
	}
	if original.Eq(&current) {
		if original.IsZero() {
			return cost + SstoreSetGas, nil
		}
		if value.IsZero() {
			evm.state.addRefund(SstoreClearsRefund)
		}
		return cost + (SstoreResetGas - ColdSloadCost), nil
	}
	// dirty slot
	if !original.IsZero() {
		if current.IsZero() {
			evm.state.subRefund(SstoreClearsRefund)
		} else if value.IsZero() {
			evm.state.addRefund(SstoreClearsRefund)
		}
	}
	if original.Eq(&value) {
		if original.IsZero() {
			evm.state.addRefund(SstoreSetGas - WarmStorageReadCost)
		} else {
			evm.state.addRefund((SstoreResetGas - ColdSloadCost) - WarmStorageReadCost)
		}
	}
	return cost + WarmStorageReadCost, nil
}

func gasTStore(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	return WarmStorageReadCost, nil
}

// --- CALL family (EIP-2929 + EIP-150 63/64) ---

func gasCallEIP2929(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	memGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return 0, err
	}
	var (
		gas            uint64
		transfersValue = !stack.back(2).IsZero()
		addr           = Address(stack.back(1).Bytes20())
	)
	if evm.state.accessAddr(addr) {
		gas += ColdAccountAccessCost
	} else {
		gas += WarmStorageReadCost
	}
	if transfersValue {
		gas += CallValueTransferGas
	}
	if transfersValue && evm.state.Empty(addr) {
		gas += CallNewAccountGas
	}
	var overflow bool
	if gas, overflow = safeAdd(gas, memGas); overflow {
		return 0, ErrGasUintOverflow
	}
	evm.callGasTemp, err = callGas(c.Gas, gas, stack.back(0))
	if err != nil {
		return 0, err
	}
	if gas, overflow = safeAdd(gas, evm.callGasTemp); overflow {
		return 0, ErrGasUintOverflow
	}
	return gas, nil
}

func gasCallCodeEIP2929(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	memGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return 0, err
	}
	var gas uint64
	addr := Address(stack.back(1).Bytes20())
	if evm.state.accessAddr(addr) {
		gas += ColdAccountAccessCost
	} else {
		gas += WarmStorageReadCost
	}
	if !stack.back(2).IsZero() {
		gas += CallValueTransferGas
	}
	var overflow bool
	if gas, overflow = safeAdd(gas, memGas); overflow {
		return 0, ErrGasUintOverflow
	}
	evm.callGasTemp, err = callGas(c.Gas, gas, stack.back(0))
	if err != nil {
		return 0, err
	}
	if gas, overflow = safeAdd(gas, evm.callGasTemp); overflow {
		return 0, ErrGasUintOverflow
	}
	return gas, nil
}

func gasDelegateOrStaticEIP2929(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	memGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return 0, err
	}
	var gas uint64
	addr := Address(stack.back(1).Bytes20())
	if evm.state.accessAddr(addr) {
		gas += ColdAccountAccessCost
	} else {
		gas += WarmStorageReadCost
	}
	var overflow bool
	if gas, overflow = safeAdd(gas, memGas); overflow {
		return 0, ErrGasUintOverflow
	}
	evm.callGasTemp, err = callGas(c.Gas, gas, stack.back(0))
	if err != nil {
		return 0, err
	}
	if gas, overflow = safeAdd(gas, evm.callGasTemp); overflow {
		return 0, ErrGasUintOverflow
	}
	return gas, nil
}

// callGas implements the EIP-150 63/64 forwarding rule.
func callGas(availableGas, base uint64, callCost *uint256.Int) (uint64, error) {
	if availableGas < base {
		return 0, ErrOutOfGas
	}
	availableGas -= base
	gas := availableGas - availableGas/CallGasCapDivisor
	if !callCost.IsUint64() || gas < callCost.Uint64() {
		return gas, nil
	}
	return callCost.Uint64(), nil
}

// --- CREATE / CREATE2 ---

func gasCreate(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	gas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return 0, err
	}
	size, overflow := stack.back(2).Uint64WithOverflow()
	if overflow || size > MaxInitCodeSize {
		return 0, ErrMaxInitCodeSizeExceeded
	}
	wordGas, o := safeMul(toWordSize(size), InitCodeWordGas)
	if o {
		return 0, ErrGasUintOverflow
	}
	if gas, overflow = safeAdd(gas, wordGas); overflow {
		return 0, ErrGasUintOverflow
	}
	return gas, nil
}

func gasCreate2(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	gas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return 0, err
	}
	size, overflow := stack.back(2).Uint64WithOverflow()
	if overflow || size > MaxInitCodeSize {
		return 0, ErrMaxInitCodeSizeExceeded
	}
	// init code words charged twice: once as init cost, once as keccak hashing
	wordGas, o := safeMul(toWordSize(size), InitCodeWordGas+Keccak256WordGas)
	if o {
		return 0, ErrGasUintOverflow
	}
	if gas, overflow = safeAdd(gas, wordGas); overflow {
		return 0, ErrGasUintOverflow
	}
	return gas, nil
}
