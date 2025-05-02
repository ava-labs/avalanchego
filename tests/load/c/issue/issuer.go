// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package issue

import (
	"context"
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/load/agent"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"go.uber.org/zap"
)

var _ agent.Issuer[*types.Transaction] = (*Issuer)(nil)

type EthClient interface {
	SendTransaction(ctx context.Context, tx *types.Transaction) error
}

type Tracker interface {
	IssueStart(txHash common.Hash)
	IssueEnd(txHash common.Hash)
}

// Issuer issues transactions to a node.
type Issuer struct {
	// Injected parameters
	client  EthClient
	tracker Tracker
	address common.Address

	// State
	lastIssuedNonce uint64 // for programming assumptions checks only
}

func New(client EthClient, tracker Tracker, address common.Address) *Issuer {
	return &Issuer{
		client:  client,
		tracker: tracker,
		address: address,
	}
}

func (i *Issuer) IssueTx(tc tests.TestContext, ctx context.Context, tx *types.Transaction) error {
	txHash, txNonce := tx.Hash(), tx.Nonce()
	if i.lastIssuedNonce > 0 && txNonce != i.lastIssuedNonce+1 {
		// the listener relies on this being true
		return fmt.Errorf("transaction nonce %d is not equal to the last issued nonce %d plus one", txNonce, i.lastIssuedNonce)
	}
	i.tracker.IssueStart(txHash)
	if err := i.client.SendTransaction(ctx, tx); err != nil && !strings.Contains(err.Error(), "already known") {
		tc.Log().Error("Failed to send transaction", zap.Error(err))
		return err
	}

	tc.Log().Info("Transaction sent",
		zap.String("sender", i.address.Hex()),
		zap.String("txHash", txHash.Hex()),
		zap.Uint64("nonce", txNonce),
	)

	i.tracker.IssueEnd(txHash)
	i.lastIssuedNonce = txNonce
	return nil
}
