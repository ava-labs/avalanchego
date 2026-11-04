package asmevm

import "github.com/holiman/uint256"

func minU64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func opCall(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	stack := scope.Stack
	stack.pop() // gas, already accounted via callGasTemp
	addr := stack.pop()
	value := stack.pop()
	inOffset := stack.pop()
	inSize := stack.pop()
	retOffset := stack.pop()
	retSize := stack.pop()
	if in.readOnly && !value.IsZero() {
		return nil, ErrWriteProtection
	}
	toAddr := Address(addr.Bytes20())
	args := scope.Memory.getPtr(inOffset.Uint64(), inSize.Uint64())
	gas := in.evm.callGasTemp
	if !value.IsZero() {
		gas += CallStipend
	}
	ret, returnGas, err := in.evm.Call(scope.Contract.self, toAddr, args, gas, &value)
	pushCallResult(stack, err)
	if err == nil || err == ErrExecutionReverted {
		n := minU64(uint64(len(ret)), retSize.Uint64())
		scope.Memory.set(retOffset.Uint64(), n, ret[:n])
	}
	scope.Contract.Gas += returnGas
	in.returnData = ret
	return ret, nil
}

func opCallCode(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	stack := scope.Stack
	stack.pop()
	addr := stack.pop()
	value := stack.pop()
	inOffset := stack.pop()
	inSize := stack.pop()
	retOffset := stack.pop()
	retSize := stack.pop()
	toAddr := Address(addr.Bytes20())
	args := scope.Memory.getPtr(inOffset.Uint64(), inSize.Uint64())
	gas := in.evm.callGasTemp
	if !value.IsZero() {
		gas += CallStipend
	}
	ret, returnGas, err := in.evm.CallCode(scope.Contract.self, toAddr, args, gas, &value)
	pushCallResult(stack, err)
	if err == nil || err == ErrExecutionReverted {
		n := minU64(uint64(len(ret)), retSize.Uint64())
		scope.Memory.set(retOffset.Uint64(), n, ret[:n])
	}
	scope.Contract.Gas += returnGas
	in.returnData = ret
	return ret, nil
}

func opDelegateCall(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	stack := scope.Stack
	stack.pop()
	addr := stack.pop()
	inOffset := stack.pop()
	inSize := stack.pop()
	retOffset := stack.pop()
	retSize := stack.pop()
	toAddr := Address(addr.Bytes20())
	args := scope.Memory.getPtr(inOffset.Uint64(), inSize.Uint64())
	gas := in.evm.callGasTemp
	ret, returnGas, err := in.evm.DelegateCall(
		scope.Contract.caller, scope.Contract.self, &scope.Contract.value,
		scope.Contract.caller, toAddr, args, gas)
	pushCallResult(stack, err)
	if err == nil || err == ErrExecutionReverted {
		n := minU64(uint64(len(ret)), retSize.Uint64())
		scope.Memory.set(retOffset.Uint64(), n, ret[:n])
	}
	scope.Contract.Gas += returnGas
	in.returnData = ret
	return ret, nil
}

func opStaticCall(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	stack := scope.Stack
	stack.pop()
	addr := stack.pop()
	inOffset := stack.pop()
	inSize := stack.pop()
	retOffset := stack.pop()
	retSize := stack.pop()
	toAddr := Address(addr.Bytes20())
	args := scope.Memory.getPtr(inOffset.Uint64(), inSize.Uint64())
	gas := in.evm.callGasTemp
	ret, returnGas, err := in.evm.StaticCall(scope.Contract.self, toAddr, args, gas)
	pushCallResult(stack, err)
	if err == nil || err == ErrExecutionReverted {
		n := minU64(uint64(len(ret)), retSize.Uint64())
		scope.Memory.set(retOffset.Uint64(), n, ret[:n])
	}
	scope.Contract.Gas += returnGas
	in.returnData = ret
	return ret, nil
}

func pushCallResult(stack *Stack, err error) {
	if err != nil {
		stack.pushN(uint256.Int{})
	} else {
		stack.pushN(*new(uint256.Int).SetOne())
	}
}

func opCreate(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	if in.readOnly {
		return nil, ErrWriteProtection
	}
	stack := scope.Stack
	value := stack.pop()
	offset := stack.pop()
	size := stack.pop()
	input := scope.Memory.getCopy(offset.Uint64(), size.Uint64())
	gas := scope.Contract.Gas
	gas -= gas / 64
	scope.Contract.useGas(gas)
	res, addr, returnGas, suberr := in.evm.Create(scope.Contract.self, input, gas, &value)
	if suberr != nil {
		stack.pushN(uint256.Int{})
	} else {
		stack.pushN(*new(uint256.Int).SetBytes(addr[:]))
	}
	scope.Contract.Gas += returnGas
	if suberr == ErrExecutionReverted {
		in.returnData = res
	} else {
		in.returnData = nil
	}
	return nil, nil
}

func opCreate2(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	if in.readOnly {
		return nil, ErrWriteProtection
	}
	stack := scope.Stack
	endowment := stack.pop()
	offset := stack.pop()
	size := stack.pop()
	salt := stack.pop()
	input := scope.Memory.getCopy(offset.Uint64(), size.Uint64())
	gas := scope.Contract.Gas
	gas -= gas / 64
	scope.Contract.useGas(gas)
	res, addr, returnGas, suberr := in.evm.Create2(scope.Contract.self, input, gas, &endowment, &salt)
	if suberr != nil {
		stack.pushN(uint256.Int{})
	} else {
		stack.pushN(*new(uint256.Int).SetBytes(addr[:]))
	}
	scope.Contract.Gas += returnGas
	if suberr == ErrExecutionReverted {
		in.returnData = res
	} else {
		in.returnData = nil
	}
	return nil, nil
}

func opSelfdestruct(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	if in.readOnly {
		return nil, ErrWriteProtection
	}
	beneficiary := scope.Stack.pop()
	benAddr := Address(beneficiary.Bytes20())
	self := scope.Contract.self
	bal := in.evm.state.Balance(self)
	// EIP-6780: transfer balance; delete only if created in this tx.
	in.evm.state.AddBalance(benAddr, &bal)
	in.evm.state.SubBalance(self, &bal)
	if a := in.evm.state.accounts[self]; a != nil && a.created {
		// geth SelfDestruct: the balance is zeroed now (burned when beneficiary == self).
		left := a.balance
		in.evm.state.SubBalance(self, &left)
		in.evm.state.markDestroyed(self)
	}
	return nil, errStopToken
}

// gasSelfdestructEIP2929 charges EIP-2929 cold access for the beneficiary.
func gasSelfdestructEIP2929(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	var gas uint64
	addr := Address(stack.back(0).Bytes20())
	if evm.state.accessAddr(addr) {
		gas += ColdAccountAccessCost
	}
	if bal := evm.state.Balance(c.self); !bal.IsZero() && evm.state.Empty(addr) {
		gas += CallNewAccountGas // CreateBySelfdestructGas
	}
	return gas, nil
}
