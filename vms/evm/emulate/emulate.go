// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package emulate provides temporary emulation of coreth (C-Chain) and
// subnet-evm (EVM L1) behaviours. All functions are safe for concurrent use
// with each other, but all hold the same mutex so their execution SHOULD be
// short-lived.
package emulate

import (
	cchain "github.com/ava-labs/coreth/plugin/evm"
	subnet "github.com/ava-labs/subnet-evm/plugin/evm"
)

// CChain executes `fn` as if running in a `coreth` node.
func CChain(fn func() error) error {
	return cchain.WithTempRegisteredLibEVMExtras(fn)
}

// SubnetEVM executes `fn` as if running in a `subnet-evm` node.
func SubnetEVM(fn func() error) error {
	return subnet.WithTempRegisteredLibEVMExtras(fn)
}

// CChainVal executes `fn` as if running in a `coreth` node.
func CChainVal[T any](fn func() (T, error)) (T, error) {
	return val(CChain, fn)
}

// SubnetEVMVal executes `fn` as if running in a `subnet-evm` node.
func SubnetEVMVal[T any](fn func() (T, error)) (T, error) {
	return val(SubnetEVM, fn)
}

func val[T any](
	wrap func(func() error) error,
	fn func() (T, error),
) (T, error) {
	var v T
	err := wrap(func() error {
		var err error
		v, err = fn()
		return err
	})
	return v, err
}
