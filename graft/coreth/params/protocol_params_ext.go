// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

const (
	// Avalanche Stateful Precompile Params
	// Gas price for native asset balance lookup. Based on the cost of an SLOAD operation since native
	// asset balances are kept in state storage.
	AssetBalanceApricot uint64 = 2100
	// Gas price for native asset call. This gas price reflects the additional work done for the native
	// asset transfer itself, which is a write to state storage. The cost of creating a new account and
	// normal value transfer is assessed separately from this cost.
	AssetCallApricot uint64 = 20000
)
