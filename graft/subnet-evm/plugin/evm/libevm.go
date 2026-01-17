// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import "github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/extras"

// RegisterAllLibEVMExtras is a convenience wrapper for calling
// [core.RegisterExtras], [customtypes.Register], and [params.RegisterExtras].
// Together these are necessary and sufficient for configuring libevm for
// Subnet-EVM behaviour.
//
// It MUST NOT be called more than once and therefore is only allowed to be used
// in tests and `package main`, to avoid polluting other packages that
// transitively depend on this one but don't need registration.
func RegisterAllLibEVMExtras() {
	extras.RegisterAllLibEVMExtras()
}

// WithTempRegisteredLibEVMExtras runs `fn` with temporary registration
// otherwise equivalent to a call to [RegisterAllLibEVMExtras], but limited to
// the life of `fn`.
func WithTempRegisteredLibEVMExtras(fn func() error) error {
	return extras.WithTempRegisteredLibEVMExtras(fn)
}
