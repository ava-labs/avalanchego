// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package libevm registers the C-Chain's libevm hooks and payloads.
package libevmextras

import (
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/cparams"
	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/libevmextras/customtypes"
)

// Register registers all the C-Chain's libevm hooks and payloads.
//
// It MUST NOT be called more than once and is therefore only allowed in tests
// and `package main`, to avoid polluting other packages that transitively
// depend on this one but don't need registration.
func Register() {
	customtypes.Register()
	vm.RegisterHooks(vmHooks{})
	state.RegisterExtras(partitionStateKeysHook{})
	cparams.RegisterExtras()
}

// WithTempRegistered runs `fn` with temporary registration otherwise
// equivalent to a call to [Register], but limited to the life of `fn`.
func WithTempRegistered(fn func() error) error {
	return libevm.WithTemporaryExtrasLock(func(lock libevm.ExtrasLock) error {
		for _, wrap := range []func(libevm.ExtrasLock, func() error) error{
			customtypes.WithTempRegisteredExtras,
			func(lock libevm.ExtrasLock, fn func() error) error {
				return vm.WithTempRegisteredHooks(lock, vmHooks{}, fn)
			},
			func(lock libevm.ExtrasLock, fn func() error) error {
				return state.WithTempRegisteredExtras(lock, partitionStateKeysHook{}, fn)
			},
			cparams.WithTempRegisteredExtras,
		} {
			inner := fn
			fn = func() error { return wrap(lock, inner) }
		}
		return fn()
	})
}
