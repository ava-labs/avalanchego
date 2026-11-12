// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package libevmextras registers the C-Chain's libevm hooks and payloads: its
// header, block body and account types, chain config and rules, and EVM and
// StateDB hooks.
package libevmextras

import (
	"math/big"

	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"

	ethtypes "github.com/ava-labs/libevm/core/types"
	ethparams "github.com/ava-labs/libevm/params"
)

// Register registers all the C-Chain's libevm hooks and payloads.
//
// It MUST NOT be called more than once and is therefore only allowed in tests
// and `package main`, to avoid polluting other packages that transitively
// depend on this one but don't need registration.
func Register() {
	registerTypes()
	vm.RegisterHooks(vmHooks{})
	state.RegisterExtras(partitionStateKeysHook{})
	registerParams()
}

// WithTempRegistered runs `fn` with temporary registration otherwise
// equivalent to a call to [Register], but limited to the life of `fn`.
func WithTempRegistered(fn func() error) error {
	return libevm.WithTemporaryExtrasLock(func(lock libevm.ExtrasLock) error {
		for _, wrap := range []func(libevm.ExtrasLock, func() error) error{
			withTempRegisteredTypes,
			func(lock libevm.ExtrasLock, fn func() error) error {
				return vm.WithTempRegisteredHooks(lock, vmHooks{}, fn)
			},
			func(lock libevm.ExtrasLock, fn func() error) error {
				return state.WithTempRegisteredExtras(lock, partitionStateKeysHook{}, fn)
			},
			withTempRegisteredParams,
		} {
			inner := fn
			fn = func() error { return wrap(lock, inner) }
		}
		return fn()
	})
}

var paramsPayloads ethparams.ExtraPayloads[*ChainConfig, Rules]

// registerParams registers hooks and payloads with libevm. It MUST NOT be
// called more than once and therefore is called by [Register].
func registerParams() {
	paramsPayloads = ethparams.RegisterExtras(paramsExtrasToRegister())
}

// withTempRegisteredParams runs `fn` with temporary registration otherwise
// equivalent to a call to [registerParams], but limited to the life of `fn`.
//
// It is called by [WithTempRegistered], along with all other
// temporary-registration functions.
func withTempRegisteredParams(lock libevm.ExtrasLock, fn func() error) error {
	old := paramsPayloads
	defer func() { paramsPayloads = old }()

	return ethparams.WithTempRegisteredExtras(
		lock, paramsExtrasToRegister(),
		func(extras ethparams.ExtraPayloads[*ChainConfig, Rules]) error {
			paramsPayloads = extras
			return fn()
		},
	)
}

func paramsExtrasToRegister() ethparams.Extras[*ChainConfig, Rules] {
	return ethparams.Extras[*ChainConfig, Rules]{
		ReuseJSONRoot: true, // Reuse the root JSON input when unmarshalling the extra payload.
		NewRules: func(_ *ethparams.ChainConfig, _ *ethparams.Rules, cEx *ChainConfig, _ *big.Int, _ bool, timestamp uint64) Rules {
			if cEx == nil {
				return Rules{}
			}
			return cEx.Rules(timestamp)
		},
	}
}

var blockPayloads ethtypes.ExtraPayloads[*Header, *BlockBody, stateAccount]

// registerTypes registers the header, block body and account payloads with
// libevm. It MUST NOT be called more than once and therefore is called by
// [Register].
//
// Without a call to [Register], none of the functionality of this package will
// work, and most will simply panic.
func registerTypes() {
	blockPayloads = ethtypes.RegisterExtras[
		Header, *Header,
		BlockBody, *BlockBody,
		stateAccount,
	]()
}

// withTempRegisteredTypes runs `fn` with temporary registration otherwise
// equivalent to a call to [registerTypes], but limited to the life of `fn`.
//
// It is called by [WithTempRegistered], along with all other
// temporary-registration functions.
func withTempRegisteredTypes(lock libevm.ExtrasLock, fn func() error) error {
	old := blockPayloads
	defer func() { blockPayloads = old }()

	return ethtypes.WithTempRegisteredExtras[Header, BlockBody, stateAccount](
		lock,
		func(e ethtypes.ExtraPayloads[*Header, *BlockBody, stateAccount]) error {
			blockPayloads = e
			return fn()
		},
	)
}
