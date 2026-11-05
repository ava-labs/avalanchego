// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package precompileconfig defines types shared by Coreth and Subnet-EVM
// precompile configurations.
package precompileconfig

// Rules defines the interface that provides information about the current rules of the chain.
type Rules interface {
	IsGraniteActivated() bool
	IsDurangoActivated() bool
}
