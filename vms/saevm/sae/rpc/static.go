// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"math/big"

	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/accounts"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
)

func (b *backend) ChainDb() ethdb.Database { //nolint:staticcheck // this name required by ethapi.Backend interface
	return b.DB()
}

func (b *backend) RPCTxFeeCap() float64 {
	return b.config.TxFeeCap
}

func (b *backend) UnprotectedAllowed() bool {
	return false
}

// ExtRPCEnabled reports that external RPC access is enabled. This adds an
// additional security measure in case we add support for the personal API.
func (*backend) ExtRPCEnabled() bool {
	return true
}

// Total difficulty does not make sense in snowman consensus, as it is not PoW.
// Ethereum, post merge (switch to PoS), sets the difficulty of each block to 0
// (see: https://github.com/ethereum/go-ethereum/blob/be92f5487e67939b8dbbc9675d6c15be76ffd18d/consensus/beacon/consensus.go#L228-L231)
// and no longer exposes the total difficulty of the chain at all via the API.
//
// TODO(JonathanOppenheimer): Once we update libevm, remove GetTd.
func (b *backend) GetTd(ctx context.Context, hash common.Hash) *big.Int {
	return new(big.Int)
}

func (b *backend) SyncProgress() ethereum.SyncProgress {
	// Avalanchego does not expose APIs until after the node has fully synced.
	return ethereum.SyncProgress{}
}

func (b *backend) AccountManager() *accounts.Manager {
	return b.accountManager
}
