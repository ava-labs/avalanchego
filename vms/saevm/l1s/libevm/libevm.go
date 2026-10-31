// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package libevm

import (
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
)

// RegisterExtras registers all the L1's libevm hooks and payloads.
//
// It MUST NOT be called more than once and is therefore only allowed in tests
// and `package main`, to avoid polluting other packages that transitively
// depend on this one but don't need registration.
func RegisterExtras() {
	core.RegisterExtras()
	customtypes.Register()
	params.RegisterExtras()
}

// WithTempRegisteredExtras runs `fn` with temporary registration otherwise
// equivalent to a call to [RegisterExtras], but limited to the life of `fn`.
func WithTempRegisteredExtras(fn func() error) error {
	return libevm.WithTemporaryExtrasLock(func(lock libevm.ExtrasLock) error {
		for _, wrap := range []func(libevm.ExtrasLock, func() error) error{
			core.WithTempRegisteredExtras,
			customtypes.WithTempRegisteredExtras,
			params.WithTempRegisteredExtras,
		} {
			inner := fn
			fn = func() error {
				return wrap(lock, inner)
			}
		}
		return fn()
	})
}
