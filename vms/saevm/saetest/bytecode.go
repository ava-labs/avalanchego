// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"slices"
	"testing"

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
	return append(Ops(vm.PUSH0+vm.OpCode(len(data))), data...)
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
