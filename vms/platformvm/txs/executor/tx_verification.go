// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"time"

	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

// memoTx is implemented by every tx type embedding [platform.BaseTx].
type memoTx interface {
	MemoData() []byte
}

// verifyTx runs the checks of tx that depend on no chain state other than the
// chain time: the upgrade activation gate, the tx's structural self-check,
// and the memo-length rule, in that order. All other checks are performed by
// each tx type's execution.
//
// It is shared by the standard and proposal executors. Each of their
// execution methods calls it before its type-specific checks, and only once
// the tx is known to belong to that executor: a tx dispatched to the wrong
// executor is rejected with errWrongTxType before any of these checks run.
func verifyTx(backend *Backend, timestamp time.Time, tx *platform.Tx) error {
	upgrades := backend.Config.UpgradeConfig
	if err := verifyTxActivation(upgrades, timestamp, tx.Unsigned); err != nil {
		return err
	}

	if err := tx.SyntacticVerify(backend.Ctx); err != nil {
		return err
	}

	memoTx, ok := tx.Unsigned.(memoTx)
	if !ok {
		return nil
	}
	return avax.VerifyMemoFieldLength(memoTx.MemoData(), upgrades.IsDurangoActivated(timestamp))
}
