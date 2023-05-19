// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import "github.com/ava-labs/avalanchego/ids"

// BootstrapTracker describes the standard interface for tracking the status of
// a subnet bootstrapping
type BootstrapTracker interface {
	// Returns true iff done bootstrapping
	IsBootstrapped() bool

	// Bootstrapped marks the named chain as being bootstrapped
	Bootstrapped(chainID ids.ID)

	OnBootstrapCompleted() chan struct{}
}
