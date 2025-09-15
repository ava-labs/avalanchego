// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keychain

// SigningOptions contains context information for signing operations
type SigningOptions struct {
	ChainAlias string
	NetworkID  uint32
}

// SigningOption is a function that modifies SigningOptions
type SigningOption func(*SigningOptions)

// WithChainAlias sets the chain alias for signing context
func WithChainAlias(chainAlias string) SigningOption {
	return func(opts *SigningOptions) {
		opts.ChainAlias = chainAlias
	}
}

// WithNetworkID sets the network ID for signing context
func WithNetworkID(networkID uint32) SigningOption {
	return func(opts *SigningOptions) {
		opts.NetworkID = networkID
	}
}