package asmevm

import "sync"

var (
	cancunTableOnce sync.Once
	cancunTable     *[256]operation
)

func cancunInstructionSet() *[256]operation {
	cancunTableOnce.Do(buildCancunTable)
	return cancunTable
}

func lim(pop, push int) (int, int) { return pop, 1024 + pop - push }

func buildCancunTable() {
	t := new([256]operation)
	for i := range t {
		t[i] = operation{execute: opUndefined, undefined: true}
	}
	set := func(op OpCode, exec executionFunc, constGas uint64, dyn gasFunc, mem memorySizeFunc, pop, push int) {
		mn, mx := lim(pop, push)
		t[op] = operation{execute: exec, constantGas: constGas, dynamicGas: dyn, memorySize: mem, minStack: mn, maxStack: mx}
	}

	set(STOP, opStop, 0, nil, nil, 0, 0)
	set(ADD, opAdd, GasFastestStep, nil, nil, 2, 1)
	set(MUL, opMul, GasFastStep, nil, nil, 2, 1)
	set(SUB, opSub, GasFastestStep, nil, nil, 2, 1)
	set(DIV, opDiv, GasFastStep, nil, nil, 2, 1)
	set(SDIV, opSdiv, GasFastStep, nil, nil, 2, 1)
	set(MOD, opMod, GasFastStep, nil, nil, 2, 1)
	set(SMOD, opSmod, GasFastStep, nil, nil, 2, 1)
	set(ADDMOD, opAddmod, GasMidStep, nil, nil, 3, 1)
	set(MULMOD, opMulmod, GasMidStep, nil, nil, 3, 1)
	set(EXP, opExp, GasSlowStep, gasExpEIP158, nil, 2, 1)
	set(SIGNEXTEND, opSignExtend, GasFastStep, nil, nil, 2, 1)

	set(LT, opLt, GasFastestStep, nil, nil, 2, 1)
	set(GT, opGt, GasFastestStep, nil, nil, 2, 1)
	set(SLT, opSlt, GasFastestStep, nil, nil, 2, 1)
	set(SGT, opSgt, GasFastestStep, nil, nil, 2, 1)
	set(EQ, opEq, GasFastestStep, nil, nil, 2, 1)
	set(ISZERO, opIszero, GasFastestStep, nil, nil, 1, 1)
	set(AND, opAnd, GasFastestStep, nil, nil, 2, 1)
	set(OR, opOr, GasFastestStep, nil, nil, 2, 1)
	set(XOR, opXor, GasFastestStep, nil, nil, 2, 1)
	set(NOT, opNot, GasFastestStep, nil, nil, 1, 1)
	set(BYTE, opByte, GasFastestStep, nil, nil, 2, 1)
	set(SHL, opSHL, GasFastestStep, nil, nil, 2, 1)
	set(SHR, opSHR, GasFastestStep, nil, nil, 2, 1)
	set(SAR, opSAR, GasFastestStep, nil, nil, 2, 1)

	set(KECCAK256, opKeccak256, Keccak256Gas, gasKeccak256, memoryKeccak256, 2, 1)

	set(ADDRESS, opAddress, GasQuickStep, nil, nil, 0, 1)
	set(BALANCE, opBalance, 0, gasEip2929AccountCheck, nil, 1, 1)
	set(ORIGIN, opOrigin, GasQuickStep, nil, nil, 0, 1)
	set(CALLER, opCaller, GasQuickStep, nil, nil, 0, 1)
	set(CALLVALUE, opCallValue, GasQuickStep, nil, nil, 0, 1)
	set(CALLDATALOAD, opCallDataLoad, GasFastestStep, nil, nil, 1, 1)
	set(CALLDATASIZE, opCallDataSize, GasQuickStep, nil, nil, 0, 1)
	set(CALLDATACOPY, opCallDataCopy, GasFastestStep, makeGasCopy(2), memoryCallDataCopy, 3, 0)
	set(CODESIZE, opCodeSize, GasQuickStep, nil, nil, 0, 1)
	set(CODECOPY, opCodeCopy, GasFastestStep, makeGasCopy(2), memoryCodeCopy, 3, 0)
	set(GASPRICE, opGasprice, GasQuickStep, nil, nil, 0, 1)
	set(EXTCODESIZE, opExtCodeSize, 0, gasEip2929AccountCheck, nil, 1, 1)
	set(EXTCODECOPY, opExtCodeCopy, 0, gasExtCodeCopyEIP2929, memoryExtCodeCopy, 4, 0)
	set(RETURNDATASIZE, opReturnDataSize, GasQuickStep, nil, nil, 0, 1)
	set(RETURNDATACOPY, opReturnDataCopy, GasFastestStep, makeGasCopy(2), memoryReturnDataCopy, 3, 0)
	set(EXTCODEHASH, opExtCodeHash, 0, gasEip2929AccountCheck, nil, 1, 1)

	set(BLOCKHASH, opBlockhash, GasExtStep, nil, nil, 1, 1)
	set(COINBASE, opCoinbase, GasQuickStep, nil, nil, 0, 1)
	set(TIMESTAMP, opTimestamp, GasQuickStep, nil, nil, 0, 1)
	set(NUMBER, opNumber, GasQuickStep, nil, nil, 0, 1)
	set(PREVRANDAO, opPrevrandao, GasQuickStep, nil, nil, 0, 1)
	set(GASLIMIT, opGasLimit, GasQuickStep, nil, nil, 0, 1)
	set(CHAINID, opChainID, GasQuickStep, nil, nil, 0, 1)
	set(SELFBALANCE, opSelfBalance, GasFastStep, nil, nil, 0, 1)
	set(BASEFEE, opBaseFee, GasQuickStep, nil, nil, 0, 1)
	set(BLOBHASH, opBlobHash, BlobHashGas, nil, nil, 1, 1)
	set(BLOBBASEFEE, opBlobBaseFee, GasQuickStep, nil, nil, 0, 1)

	set(POP, opPop, GasQuickStep, nil, nil, 1, 0)
	set(MLOAD, opMload, GasFastestStep, gasMLoadStore, memoryMLoad, 1, 1)
	set(MSTORE, opMstore, GasFastestStep, gasMLoadStore, memoryMStore, 2, 0)
	set(MSTORE8, opMstore8, GasFastestStep, gasMLoadStore, memoryMStore8, 2, 0)
	set(SLOAD, opSload, 0, gasSLoadEIP2929, nil, 1, 1)
	set(SSTORE, opSstore, 0, gasSStoreEIP2929, nil, 2, 0)
	set(JUMP, opJump, GasMidStep, nil, nil, 1, 0)
	set(JUMPI, opJumpi, GasSlowStep, nil, nil, 2, 0)
	set(PC, opPc, GasQuickStep, nil, nil, 0, 1)
	set(MSIZE, opMsize, GasQuickStep, nil, nil, 0, 1)
	set(GAS, opGas, GasQuickStep, nil, nil, 0, 1)
	set(JUMPDEST, opJumpdest, JumpdestGas, nil, nil, 0, 0)
	set(TLOAD, opTload, WarmStorageReadCost, nil, nil, 1, 1)
	set(TSTORE, opTstore, WarmStorageReadCost, nil, nil, 2, 0)
	set(MCOPY, opMcopy, GasFastestStep, gasMcopy, memoryMcopy, 3, 0)
	set(PUSH0, opPush0, GasQuickStep, nil, nil, 0, 1)

	for i := 0; i < 32; i++ {
		set(PUSH1+OpCode(i), makePush(uint64(i+1), i+1), GasFastestStep, nil, nil, 0, 1)
	}
	for i := 0; i < 16; i++ {
		set(DUP1+OpCode(i), makeDup(i+1), GasFastestStep, nil, nil, i+1, i+2)
		set(SWAP1+OpCode(i), makeSwap(i+1), GasFastestStep, nil, nil, i+2, i+2)
	}
	for i := 0; i < 5; i++ {
		set(LOG0+OpCode(i), makeLog(i), 0, makeGasLog(uint64(i)), memoryLog, i+2, 0)
	}

	set(CREATE, opCreate, CreateGas, gasCreate, memoryCreate, 3, 1)
	set(CALL, opCall, 0, gasCallEIP2929, memoryCall, 7, 1)
	set(CALLCODE, opCallCode, 0, gasCallCodeEIP2929, memoryCall, 7, 1)
	set(RETURN, opReturn, 0, gasMLoadStore, memoryReturn, 2, 0)
	set(DELEGATECALL, opDelegateCall, 0, gasDelegateOrStaticEIP2929, memoryDelegateCall, 6, 1)
	set(CREATE2, opCreate2, CreateGas, gasCreate2, memoryCreate2, 4, 1)
	set(STATICCALL, opStaticCall, 0, gasDelegateOrStaticEIP2929, memoryDelegateCall, 6, 1)
	set(REVERT, opRevert, 0, gasMLoadStore, memoryRevert, 2, 0)
	set(INVALID, opInvalid, 0, nil, nil, 0, 0)
	set(SELFDESTRUCT, opSelfdestruct, SelfdestructGas, gasSelfdestructEIP2929, nil, 1, 0)

	cancunTable = t
}
