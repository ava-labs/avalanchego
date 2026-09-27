// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/pchain"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

// fundPChainAddress sends amount to addr on the P-Chain and waits for every
// node to confirm it.
func (w *workload) fundPChainAddress(ctx context.Context, addr ids.ShortID, amount uint64) error {
	pWallet := w.wallet.P()
	tx, err := pWallet.IssueBaseTx([]*avax.TransferableOutput{{
		Asset: avax.Asset{
			ID: pWallet.Builder().Context().AVAXAssetID,
		},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr},
			},
		},
	}})
	if err != nil {
		return fmt.Errorf("failed to issue P-chain funding baseTx: %w", err)
	}
	w.log.Info("issued P-chain funding baseTx",
		zap.Stringer("txID", tx.ID()),
		zap.Stringer("addr", addr),
	)
	return w.confirmPChainTx(ctx, tx)
}

// issuePChainAddValidatorTx registers a validator with a made-up node ID
// staking the minimum amount. No process runs for that ID, so the validator
// never votes. Keep the action's run limit small so these validators never
// hold enough of the total stake to cost consensus its quorum.
func (w *workload) issuePChainAddValidatorTx(ctx context.Context) {
	issueCtx, cancel := context.WithTimeout(ctx, txConfirmationTimeout)
	defer cancel()

	rewardAddr, _ := w.addrs.Peek()
	nodeID := ids.NodeID(utils.RandomBytes(ids.NodeIDLen))
	tx, err := pchain.IssueAddValidatorTx(issueCtx, w.wallet.P(), w.pChainNode, nodeID, rewardAddr, rewardAddr)
	w.recordPChainResult(ctx, tx, err)
}

func (w *workload) issuePChainAddDelegatorTx(ctx context.Context) {
	issueCtx, cancel := context.WithTimeout(ctx, txConfirmationTimeout)
	defer cancel()

	rewardAddr, _ := w.addrs.Peek()
	tx, err := pchain.IssueAddDelegatorTx(issueCtx, w.wallet.P(), w.pChainNode, w.pChainNode.NodeID, time.Time{}, rewardAddr)
	w.recordPChainResult(ctx, tx, err)
}

// recordPChainResult logs the outcome of an action and hands any produced
// transaction to the monitor, which owns commitment and durability checks.
func (w *workload) recordPChainResult(ctx context.Context, tx *platform.Tx, err error) {
	fields := []zap.Field{zap.Stringer("node", w.pChainNode)}
	var txType string
	if tx != nil {
		txType = fmt.Sprintf("%T", tx.Unsigned)
		fields = append(fields, zap.Stringer("txID", tx.ID()), zap.String("txType", txType))
	}
	switch {
	case errors.Is(err, builder.ErrInsufficientFunds):
		w.log.Info("skipping P-chain action due to insufficient funds", fields...)
	case err != nil:
		w.log.Warn("failed to issue P-chain transaction", append(fields, zap.Error(err))...)
	default:
		w.log.Info("issued P-chain transaction", fields...)
	}
	if tx == nil {
		return
	}
	w.pChainMonitor.recordTx(pChainTx{
		id: tx.ID(), txType: txType, workerID: w.id, issuer: w.pChainNode,
	})
	if err != nil {
		// The transaction may still commit and spend the wallet's cached inputs.
		w.refreshPrimaryWallet(ctx)
	}
}

// refreshPrimaryWallet rebuilds the wallet's UTXO view from the node.
func (w *workload) refreshPrimaryWallet(ctx context.Context) {
	// The issuance context may have expired, so bound the refresh on its own.
	ctx, cancel := context.WithTimeout(ctx, txConfirmationTimeout)
	defer cancel()
	refreshed, err := e2e.MakeWallet(
		ctx,
		w.log,
		w.keychain,
		tmpnet.NodeURI{NodeID: w.pChainNode.NodeID, URI: w.pChainNode.URI},
		primary.WalletConfig{},
	)
	if err != nil {
		w.log.Warn("failed to refresh primary wallet", zap.Error(err))
		return
	}
	w.wallet = refreshed
}
