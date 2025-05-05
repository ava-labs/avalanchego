// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package issue

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

type EthClient interface {
	SendTransaction(ctx context.Context, tx *types.Transaction) error
}

type Tracker interface {
	Issue(txHash common.Hash)
}

// Issuer issues transactions to a node.
type Issuer struct {
	// Injected parameters
	client      EthClient
	tracker     Tracker
	issuePeriod time.Duration

	// State
	lastIssue       time.Time
	lastIssuedNonce uint64 // for programming assumptions checks only
}

func New(client EthClient, tracker Tracker, issuePeriod time.Duration) *Issuer {
	return &Issuer{
		client:      client,
		tracker:     tracker,
		issuePeriod: issuePeriod,
	}
}

func (i *Issuer) IssueTx(ctx context.Context, tx *types.Transaction) error {
	if i.issuePeriod > 0 && !i.lastIssue.IsZero() &&
		time.Since(i.lastIssue) < i.issuePeriod {
		timer := time.NewTimer(i.issuePeriod - time.Since(i.lastIssue))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}

	txHash, txNonce := tx.Hash(), tx.Nonce()
	if i.lastIssuedNonce > 0 && txNonce != i.lastIssuedNonce+1 {
		// the listener relies on this being true
		return fmt.Errorf("transaction nonce %d is not equal to the last issued nonce %d plus one", txNonce, i.lastIssuedNonce)
	}
	if err := i.client.SendTransaction(ctx, tx); err != nil {
		return fmt.Errorf("issuing transaction with nonce %d: %w", txNonce, err)
	}
	i.tracker.Issue(txHash)
	i.lastIssuedNonce = txNonce
	i.lastIssue = time.Now()
	return nil
}
