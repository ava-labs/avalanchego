// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package backends

import (
	"context"

	"github.com/ava-labs/avalanchego/graft/coreth/accounts/abi/bind"
	"github.com/ava-labs/avalanchego/graft/coreth/ethclient/simulated"
	"github.com/ava-labs/avalanchego/graft/coreth/interfaces"
	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

// Verify that SimulatedBackend implements required interfaces
var (
	_ bind.AcceptedContractCaller = (*SimulatedBackend)(nil)
	_ bind.ContractBackend        = (*SimulatedBackend)(nil)
	_ bind.DeployBackend          = (*SimulatedBackend)(nil)

	_ ethereum.ChainReader              = (*SimulatedBackend)(nil)
	_ ethereum.ChainStateReader         = (*SimulatedBackend)(nil)
	_ ethereum.TransactionReader        = (*SimulatedBackend)(nil)
	_ ethereum.TransactionSender        = (*SimulatedBackend)(nil)
	_ ethereum.ContractCaller           = (*SimulatedBackend)(nil)
	_ ethereum.GasEstimator             = (*SimulatedBackend)(nil)
	_ ethereum.GasPricer                = (*SimulatedBackend)(nil)
	_ ethereum.LogFilterer              = (*SimulatedBackend)(nil)
	_ interfaces.AcceptedStateReader    = (*SimulatedBackend)(nil)
	_ interfaces.AcceptedContractCaller = (*SimulatedBackend)(nil)
)

// SimulatedBackend is a simulated blockchain.
// Deprecated: use package github.com/ava-labs/avalanchego/graft/coreth/ethclient/simulated instead.
type SimulatedBackend struct {
	*simulated.Backend
	simulated.Client
}

// Fork sets the head to a new block, which is based on the provided parentHash.
func (b *SimulatedBackend) Fork(ctx context.Context, parentHash common.Hash) error {
	return b.Backend.Fork(parentHash)
}

// NewSimulatedBackend creates a new binding backend using a simulated blockchain
// for testing purposes.
//
// A simulated backend always uses chainID 1337.
//
// Deprecated: please use simulated.Backend from package
// github.com/ava-labs/avalanchego/graft/coreth/ethclient/simulated instead.
func NewSimulatedBackend(alloc types.GenesisAlloc, gasLimit uint64) *SimulatedBackend {
	b := simulated.NewBackend(alloc, simulated.WithBlockGasLimit(gasLimit))
	return &SimulatedBackend{
		Backend: b,
		Client:  b.Client(),
	}
}
