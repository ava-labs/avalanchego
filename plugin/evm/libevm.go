// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/extstate"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
)

// RegisterAllLibEVMExtras is a convenience wrapper for calling
// [core.RegisterExtras], [customtypes.Register], [extstate.RegisterExtras], and
// [params.RegisterExtras]. Together these are necessary and sufficient for
// configuring libevm for C-Chain behaviour.
//
// It MUST NOT be called more than once and therefore is only allowed to be used
// in tests and `package main`, to avoid polluting other packages that
// transitively depend on this one but don't need registration.
func RegisterAllLibEVMExtras() {
	core.RegisterExtras()
	customtypes.Register()
	extstate.RegisterExtras()
	params.RegisterExtras()
}

// WithTempRegisteredLibEVMExtras runs `fn` with temporary registration
// otherwise equivalent to a call to [RegisterAllLibEVMExtras], but limited to
// the life of `fn`.
func WithTempRegisteredLibEVMExtras(fn func() error) error {
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
