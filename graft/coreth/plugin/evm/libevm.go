// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import cchainlibevm "github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm"

// RegisterAllLibEVMExtras registers the C-Chain hooks and payloads with
// libevm. See [cchainlibevm.RegisterExtras].
func RegisterAllLibEVMExtras() {
	cchainlibevm.RegisterExtras()
}

// WithTempRegisteredLibEVMExtras runs `fn` with temporary registration
// otherwise equivalent to a call to [RegisterAllLibEVMExtras], but limited to
// the life of `fn`.
func WithTempRegisteredLibEVMExtras(fn func() error) error {
	return cchainlibevm.WithTempRegisteredExtras(fn)
}
