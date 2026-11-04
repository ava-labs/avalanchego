package asmevm

import "github.com/holiman/uint256"

// --- arithmetic ---

func opAdd(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	y.Add(&x, y)
	return nil, nil
}
func opSub(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	y.Sub(&x, y)
	return nil, nil
}
func opMul(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	y.Mul(&x, y)
	return nil, nil
}
func opDiv(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	y.Div(&x, y)
	return nil, nil
}
func opSdiv(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	y.SDiv(&x, y)
	return nil, nil
}
func opMod(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	y.Mod(&x, y)
	return nil, nil
}
func opSmod(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	y.SMod(&x, y)
	return nil, nil
}
func opAddmod(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.pop()
	z := scope.Stack.peek()
	z.AddMod(&x, &y, z)
	return nil, nil
}
func opMulmod(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.pop()
	z := scope.Stack.peek()
	z.MulMod(&x, &y, z)
	return nil, nil
}
func opExp(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	base := scope.Stack.pop()
	exp := scope.Stack.peek()
	exp.Exp(&base, exp)
	return nil, nil
}
func opSignExtend(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	back := scope.Stack.pop()
	num := scope.Stack.peek()
	num.ExtendSign(num, &back)
	return nil, nil
}

// --- comparison / bitwise ---

func opLt(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	if x.Lt(y) {
		y.SetOne()
	} else {
		y.Clear()
	}
	return nil, nil
}
func opGt(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	if x.Gt(y) {
		y.SetOne()
	} else {
		y.Clear()
	}
	return nil, nil
}
func opSlt(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	if x.Slt(y) {
		y.SetOne()
	} else {
		y.Clear()
	}
	return nil, nil
}
func opSgt(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	if x.Sgt(y) {
		y.SetOne()
	} else {
		y.Clear()
	}
	return nil, nil
}
func opEq(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	if x.Eq(y) {
		y.SetOne()
	} else {
		y.Clear()
	}
	return nil, nil
}
func opIszero(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.peek()
	if x.IsZero() {
		x.SetOne()
	} else {
		x.Clear()
	}
	return nil, nil
}
func opAnd(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	y.And(&x, y)
	return nil, nil
}
func opOr(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	y.Or(&x, y)
	return nil, nil
}
func opXor(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.pop()
	y := scope.Stack.peek()
	y.Xor(&x, y)
	return nil, nil
}
func opNot(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.peek()
	x.Not(x)
	return nil, nil
}
func opByte(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	th := scope.Stack.pop()
	val := scope.Stack.peek()
	val.Byte(&th)
	return nil, nil
}
func opSHL(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	shift := scope.Stack.pop()
	value := scope.Stack.peek()
	if shift.LtUint64(256) {
		value.Lsh(value, uint(shift.Uint64()))
	} else {
		value.Clear()
	}
	return nil, nil
}
func opSHR(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	shift := scope.Stack.pop()
	value := scope.Stack.peek()
	if shift.LtUint64(256) {
		value.Rsh(value, uint(shift.Uint64()))
	} else {
		value.Clear()
	}
	return nil, nil
}
func opSAR(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	shift := scope.Stack.pop()
	value := scope.Stack.peek()
	if shift.GtUint64(255) {
		if value.Sign() >= 0 {
			value.Clear()
		} else {
			value.SetAllOne()
		}
		return nil, nil
	}
	value.SRsh(value, uint(shift.Uint64()))
	return nil, nil
}

func opKeccak256(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	offset := scope.Stack.pop()
	size := scope.Stack.peek()
	data := scope.Memory.getPtr(offset.Uint64(), size.Uint64())
	if traceKeccak != nil {
		traceKeccak(in, data)
	}
	h := Keccak256(data)
	size.SetBytes32(h[:])
	return nil, nil
}

// --- environment ---

func opAddress(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(*new(uint256.Int).SetBytes(scope.Contract.self[:]))
	return nil, nil
}
func opBalance(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	slot := scope.Stack.peek()
	addr := Address(slot.Bytes20())
	if addr == in.evm.Tx.Origin {
		in.evm.state.FeeReads |= FeeReadPrice
	}
	bal := in.evm.state.Balance(addr)
	slot.Set(&bal)
	return nil, nil
}
func opOrigin(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(*new(uint256.Int).SetBytes(in.evm.Tx.Origin[:]))
	return nil, nil
}
func opCaller(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(*new(uint256.Int).SetBytes(scope.Contract.caller[:]))
	return nil, nil
}
func opCallValue(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(scope.Contract.value)
	return nil, nil
}
func opCallDataLoad(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	x := scope.Stack.peek()
	if off, ok := x.Uint64WithOverflow(); !ok {
		data := getData(scope.Contract.Input, off, 32)
		x.SetBytes(data)
	} else {
		x.Clear()
	}
	return nil, nil
}
func opCallDataSize(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(*new(uint256.Int).SetUint64(uint64(len(scope.Contract.Input))))
	return nil, nil
}
func opCallDataCopy(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	var (
		memOffset  = scope.Stack.pop()
		dataOffset = scope.Stack.pop()
		length     = scope.Stack.pop()
	)
	dataOff64, overflow := dataOffset.Uint64WithOverflow()
	if overflow {
		dataOff64 = ^uint64(0)
	}
	scope.Memory.set(memOffset.Uint64(), length.Uint64(), getData(scope.Contract.Input, dataOff64, length.Uint64()))
	return nil, nil
}
func opCodeSize(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(*new(uint256.Int).SetUint64(uint64(len(scope.Contract.Code))))
	return nil, nil
}
func opCodeCopy(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	var (
		memOffset  = scope.Stack.pop()
		codeOffset = scope.Stack.pop()
		length     = scope.Stack.pop()
	)
	co, overflow := codeOffset.Uint64WithOverflow()
	if overflow {
		co = ^uint64(0)
	}
	scope.Memory.set(memOffset.Uint64(), length.Uint64(), getData(scope.Contract.Code, co, length.Uint64()))
	return nil, nil
}
func opGasprice(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	in.evm.state.FeeReads |= FeeReadPrice
	scope.Stack.pushN(in.evm.Tx.GasPrice)
	return nil, nil
}
func opExtCodeSize(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	slot := scope.Stack.peek()
	addr := Address(slot.Bytes20())
	slot.SetUint64(uint64(in.evm.state.CodeSize(addr)))
	return nil, nil
}
func opExtCodeCopy(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	var (
		stack      = scope.Stack
		a          = stack.pop()
		memOffset  = stack.pop()
		codeOffset = stack.pop()
		length     = stack.pop()
	)
	co, overflow := codeOffset.Uint64WithOverflow()
	if overflow {
		co = ^uint64(0)
	}
	addr := Address(a.Bytes20())
	code := in.evm.state.Code(addr)
	scope.Memory.set(memOffset.Uint64(), length.Uint64(), getData(code, co, length.Uint64()))
	return nil, nil
}
func opReturnDataSize(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(*new(uint256.Int).SetUint64(uint64(len(in.returnData))))
	return nil, nil
}
func opReturnDataCopy(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	var (
		memOffset  = scope.Stack.pop()
		dataOffset = scope.Stack.pop()
		length     = scope.Stack.pop()
	)
	offset64, overflow := dataOffset.Uint64WithOverflow()
	if overflow {
		return nil, ErrReturnDataOutOfBounds
	}
	end, overflow := new(uint256.Int).AddOverflow(&dataOffset, &length)
	if overflow {
		return nil, ErrReturnDataOutOfBounds
	}
	end64, overflow := end.Uint64WithOverflow()
	if overflow || end64 > uint64(len(in.returnData)) {
		return nil, ErrReturnDataOutOfBounds
	}
	scope.Memory.set(memOffset.Uint64(), length.Uint64(), in.returnData[offset64:end64])
	return nil, nil
}
func opExtCodeHash(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	slot := scope.Stack.peek()
	addr := Address(slot.Bytes20())
	if in.evm.state.Empty(addr) {
		slot.Clear()
	} else {
		h := in.evm.state.CodeHash(addr)
		slot.SetBytes(h[:])
	}
	return nil, nil
}

// --- block ---

func opBlockhash(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	num := scope.Stack.peek()
	num64, overflow := num.Uint64WithOverflow()
	if overflow {
		num.Clear()
		return nil, nil
	}
	cur := in.evm.Block.BlockNumber
	if num64 >= cur || cur-num64 > 256 || num64 == 0 && cur == 0 {
		num.Clear()
		return nil, nil
	}
	h, ok := in.evm.Block.BlockHashes[num64]
	if !ok && in.evm.Block.OnHashMiss != nil {
		in.evm.Block.OnHashMiss(num64)
		h, ok = in.evm.Block.BlockHashes[num64] // a lazy context (eth_call) fills it
	}
	if ok {
		num.SetBytes(h[:])
	} else {
		num.Clear()
	}
	return nil, nil
}
func opCoinbase(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(*new(uint256.Int).SetBytes(in.evm.Block.Coinbase[:]))
	return nil, nil
}
func opTimestamp(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(*new(uint256.Int).SetUint64(in.evm.Block.Time))
	return nil, nil
}
func opNumber(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(*new(uint256.Int).SetUint64(in.evm.Block.BlockNumber))
	return nil, nil
}
func opPrevrandao(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(*new(uint256.Int).SetBytes(in.evm.Block.Random[:]))
	return nil, nil
}
func opGasLimit(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(*new(uint256.Int).SetUint64(in.evm.Block.GasLimit))
	return nil, nil
}
func opChainID(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(in.evm.chainID)
	return nil, nil
}
func opSelfBalance(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	if scope.Contract.self == in.evm.Tx.Origin {
		in.evm.state.FeeReads |= FeeReadPrice
	}
	bal := in.evm.state.Balance(scope.Contract.self)
	scope.Stack.pushN(bal)
	return nil, nil
}
func opBaseFee(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	in.evm.state.FeeReads |= FeeReadBaseFee
	scope.Stack.pushN(in.evm.Block.BaseFee)
	return nil, nil
}
func opBlobHash(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	idx := scope.Stack.peek()
	if idx.LtUint64(uint64(len(in.evm.Tx.BlobHashes))) {
		h := in.evm.Tx.BlobHashes[idx.Uint64()]
		idx.SetBytes(h[:])
	} else {
		idx.Clear()
	}
	return nil, nil
}
func opBlobBaseFee(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(in.evm.Block.BlobBaseFee)
	return nil, nil
}

// --- stack, memory, storage, flow ---

func opPop(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pop()
	return nil, nil
}
func opMload(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	v := scope.Stack.peek()
	offset := v.Uint64()
	v.SetBytes(scope.Memory.getPtr(offset, 32))
	return nil, nil
}
func opMstore(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	mStart := scope.Stack.pop()
	val := scope.Stack.pop()
	scope.Memory.set32(mStart.Uint64(), &val)
	return nil, nil
}
func opMstore8(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	off := scope.Stack.pop()
	val := scope.Stack.pop()
	scope.Memory.store[off.Uint64()] = byte(val.Uint64())
	return nil, nil
}
func opSload(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	loc := scope.Stack.peek()
	slot := Hash(loc.Bytes32())
	v := in.evm.state.GetStorage(scope.Contract.self, slot)
	loc.Set(&v)
	return nil, nil
}
func opSstore(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	if in.readOnly {
		return nil, ErrWriteProtection
	}
	loc := scope.Stack.pop()
	val := scope.Stack.pop()
	slot := Hash(loc.Bytes32())
	in.evm.state.SetStorage(scope.Contract.self, slot, val)
	return nil, nil
}
func opJump(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	dest := scope.Stack.pop()
	if !scope.Contract.validJumpdest(&dest) {
		return nil, ErrInvalidJump
	}
	*pc = dest.Uint64() - 1
	return nil, nil
}
func opJumpi(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	dest := scope.Stack.pop()
	cond := scope.Stack.pop()
	if !cond.IsZero() {
		if !scope.Contract.validJumpdest(&dest) {
			return nil, ErrInvalidJump
		}
		*pc = dest.Uint64() - 1
	}
	return nil, nil
}
func opPc(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(*new(uint256.Int).SetUint64(*pc))
	return nil, nil
}
func opMsize(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(*new(uint256.Int).SetUint64(uint64(scope.Memory.len())))
	return nil, nil
}
func opGas(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(*new(uint256.Int).SetUint64(scope.Contract.Gas))
	return nil, nil
}
func opJumpdest(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) { return nil, nil }

func opTload(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	loc := scope.Stack.peek()
	slot := Hash(loc.Bytes32())
	v := in.evm.state.GetTransient(scope.Contract.self, slot)
	loc.Set(&v)
	return nil, nil
}
func opTstore(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	if in.readOnly {
		return nil, ErrWriteProtection
	}
	loc := scope.Stack.pop()
	val := scope.Stack.pop()
	slot := Hash(loc.Bytes32())
	in.evm.state.SetTransient(scope.Contract.self, slot, val)
	return nil, nil
}
func opMcopy(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	var (
		dst    = scope.Stack.pop()
		src    = scope.Stack.pop()
		length = scope.Stack.pop()
	)
	if length.IsZero() {
		return nil, nil
	}
	copy(scope.Memory.store[dst.Uint64():dst.Uint64()+length.Uint64()],
		scope.Memory.getPtr(src.Uint64(), length.Uint64()))
	return nil, nil
}
func opPush0(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	scope.Stack.pushN(uint256.Int{})
	return nil, nil
}

func opReturn(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	offset := scope.Stack.pop()
	size := scope.Stack.pop()
	ret := scope.Memory.getCopy(offset.Uint64(), size.Uint64())
	return ret, errStopToken
}
func opRevert(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	offset := scope.Stack.pop()
	size := scope.Stack.pop()
	ret := scope.Memory.getCopy(offset.Uint64(), size.Uint64())
	in.returnData = ret
	return ret, ErrExecutionReverted
}
func opStop(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	return nil, errStopToken
}
func opUndefined(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	return nil, errInvalidOp(OpCode(scope.Contract.Code[*pc]))
}
func opInvalid(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	return nil, errInvalidOp(INVALID)
}

// makePush returns the execute function for PUSH1..PUSH32.
func makePush(size uint64, pushByteSize int) executionFunc {
	return func(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
		code := scope.Contract.Code
		var v uint256.Int
		start := *pc + 1
		if start+size <= uint64(len(code)) {
			v.SetBytes(code[start : start+size])
		} else {
			var buf [32]byte
			for i := uint64(0); i < size && start+i < uint64(len(code)); i++ {
				buf[i] = code[start+i]
			}
			v.SetBytes(buf[:size])
		}
		scope.Stack.pushN(v)
		*pc += size
		return nil, nil
	}
}
func makeDup(n int) executionFunc {
	return func(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
		scope.Stack.dup(n)
		return nil, nil
	}
}
func makeSwap(n int) executionFunc {
	return func(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
		scope.Stack.swap(n)
		return nil, nil
	}
}
func makeLog(size int) executionFunc {
	return func(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
		if in.readOnly {
			return nil, ErrWriteProtection
		}
		topics := make([]Hash, size)
		stack := scope.Stack
		mStart := stack.pop()
		mSize := stack.pop()
		for i := 0; i < size; i++ {
			addr := stack.pop()
			topics[i] = Hash(addr.Bytes32())
		}
		data := scope.Memory.getCopy(mStart.Uint64(), mSize.Uint64())
		in.evm.state.addLog(Log{Address: scope.Contract.self, Topics: topics, Data: data})
		return nil, nil
	}
}
