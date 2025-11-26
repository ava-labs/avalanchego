// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

type WalletService struct {
	vm         *VM
	pendingTxs *linked.Hashmap[ids.ID, *txs.Tx]
}

func (w *WalletService) decided(txID ids.ID) {
	if !w.pendingTxs.Delete(txID) {
		return
	}

	w.vm.ctx.Log.Info("tx decided over wallet API",
		zap.Stringer("txID", txID),
	)
	for {
		txID, tx, ok := w.pendingTxs.Oldest()
		if !ok {
			return
		}

		err := w.vm.network.IssueTxFromRPCWithoutVerification(tx)
		if err == nil {
			w.vm.ctx.Log.Info("issued tx to mempool over wallet API",
				zap.Stringer("txID", txID),
			)
			return
		}

		w.pendingTxs.Delete(txID)
		w.vm.ctx.Log.Warn("dropping tx issued over wallet API",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)
	}
}

func (w *WalletService) issue(tx *txs.Tx) (ids.ID, error) {
	txID := tx.ID()
	w.vm.ctx.Log.Info("issuing tx over wallet API",
		zap.Stringer("txID", txID),
	)

	if _, ok := w.pendingTxs.Get(txID); ok {
		w.vm.ctx.Log.Warn("issuing duplicate tx over wallet API",
			zap.Stringer("txID", txID),
		)
		return txID, nil
	}

	if w.pendingTxs.Len() == 0 {
		if err := w.vm.network.IssueTxFromRPCWithoutVerification(tx); err != nil {
			w.vm.ctx.Log.Warn("failed to issue tx over wallet API",
				zap.Stringer("txID", txID),
				zap.Error(err),
			)
			return ids.Empty, err
		}
		w.vm.ctx.Log.Info("issued tx to mempool over wallet API",
			zap.Stringer("txID", txID),
		)
	} else {
		w.vm.ctx.Log.Info("enqueueing tx over wallet API",
			zap.Stringer("txID", txID),
		)
	}

	w.pendingTxs.Put(txID, tx)
	return txID, nil
}

// IssueTx attempts to issue a transaction into consensus
func (w *WalletService) IssueTx(_ *http.Request, args *api.FormattedTx, reply *api.JSONTxID) error {
	w.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "wallet"),
		zap.String("method", "issueTx"),
		logging.UserString("tx", args.Tx),
	)

	txBytes, err := formatting.Decode(args.Encoding, args.Tx)
	if err != nil {
		return fmt.Errorf("problem decoding transaction: %w", err)
	}

	tx, err := w.vm.parser.ParseTx(txBytes)
	if err != nil {
		return err
	}

	w.vm.ctx.Lock.Lock()
	defer w.vm.ctx.Lock.Unlock()

	txID, err := w.issue(tx)
	reply.TxID = txID
	return err
}
