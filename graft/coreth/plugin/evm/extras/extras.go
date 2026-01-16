// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package extras provides libevm type registration for C-Chain behaviour.
// This package is intentionally kept separate from the main evm package to
// avoid import cycles - the evm package imports rpc, but many packages need
// to register extras without importing the full evm package.
package extras

import (
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
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
