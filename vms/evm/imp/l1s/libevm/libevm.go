// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package libevm registers the L1 VM's libevm hooks and payloads.
package libevm

import (
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
)

// RegisterExtras registers all the L1 VM's libevm hooks and payloads.
//
// It MUST NOT be called more than once and is therefore only allowed in tests
// and `package main`, to avoid polluting other packages that transitively
// depend on this one but don't need registration.
func RegisterExtras() {
	// TODO: Register the precompile modules (e.g. by importing
	// `precompile/registry`) once the VM supports precompiles.
	core.RegisterExtras()
	customtypes.Register()
	params.RegisterExtras()
}
