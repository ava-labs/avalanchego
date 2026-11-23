// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"
)

// Ops converts opcodes to bytecode. Push operands MAY be interspersed with the
// opcodes as untyped constants; for multi-byte operands, prefer [Push].
func Ops(ops ...vm.OpCode) []byte {
	buf := make([]byte, len(ops))
	for i, o := range ops {
		buf[i] = byte(o)
	}
	return buf
}

// Push returns bytecode that pushes data onto the stack as a single
// PUSH<len(data)> instruction, failing tb if data is empty or exceeds the
// EVM's 32-byte push limit. To push a zero, use [Ops] with [vm.PUSH0].
func Push(tb testing.TB, data []byte) []byte {
	tb.Helper()
	require.NotEmpty(tb, data, "Push() data is empty; PUSH0 pushes a 0, not zero-length data")
	require.LessOrEqualf(tb, len(data), 32, "Push() data length exceeds the EVM's push limit")
	return append(Ops(vm.PUSH0+vm.OpCode(len(data))), data...) //#nosec G115 -- the require above limits len(data) to 32.
}

// LogTopOfStackAfter returns runtime bytecode that executes the concatenated
// fragments and then emits its sole log, with the value on top of the stack
// as the only topic.
func LogTopOfStackAfter(fragments ...[]byte) []byte {
	return slices.Concat(
		slices.Concat(fragments...),
		Ops(
			vm.PUSH0, vm.PUSH0, // size + offset
			vm.LOG1,
			vm.STOP,
		),
	)
}

// ForwardAndLogCode returns runtime bytecode that forwards its calldata to
// callee and emits the returned data as a log.
func ForwardAndLogCode(tb testing.TB, callee common.Address) []byte {
	tb.Helper()
	return slices.Concat(
		// mem[0:cds) = calldata
		Ops(vm.CALLDATASIZE, vm.PUSH0, vm.PUSH0, vm.CALLDATACOPY),
		// call callee with mem[0:cds) as input
		Ops(vm.PUSH0, vm.PUSH0),        // return size + offset; read via RETURNDATACOPY instead
		Ops(vm.CALLDATASIZE, vm.PUSH0), // input size + offset
		Ops(vm.PUSH0),                  // value
		Push(tb, callee[:]),
		Ops(vm.GAS, vm.CALL),
		Ops(vm.POP), // discard the success flag
		// mem[0:rds) = return data
		Ops(vm.RETURNDATASIZE, vm.PUSH0, vm.PUSH0, vm.RETURNDATACOPY),
		// log0(mem[0:rds))
		Ops(vm.RETURNDATASIZE, vm.PUSH0, vm.LOG0),
		Ops(vm.STOP),
	)
}
