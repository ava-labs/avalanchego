// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package libevm registers the C-Chain's libevm hooks and payloads.
package libevm

import (
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/extstate"
)

// RegisterExtras registers all the C-Chain's libevm hooks and payloads.
//
// It MUST NOT be called more than once and is therefore only allowed in tests
// and `package main`, to avoid polluting other packages that transitively
// depend on this one but don't need registration.
func RegisterExtras() {
	core.RegisterExtras()
	customtypes.Register()
	extstate.RegisterExtras()
	params.RegisterExtras()
}

// WithTempRegisteredExtras runs `fn` with temporary registration otherwise
// equivalent to a call to [RegisterExtras], but limited to the life of `fn`.
func WithTempRegisteredExtras(fn func() error) error {
	return libevm.WithTemporaryExtrasLock(func(lock libevm.ExtrasLock) error {
		for _, wrap := range []func(libevm.ExtrasLock, func() error) error{
			core.WithTempRegisteredExtras,
			customtypes.WithTempRegisteredExtras,
			extstate.WithTempRegisteredExtras,
			params.WithTempRegisteredExtras,
		} {
			inner := fn
			fn = func() error { return wrap(lock, inner) }
		}
		return fn()
	})
}
