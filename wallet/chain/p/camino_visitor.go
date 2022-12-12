// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func (b *backendVisitor) AddAddressStateTx(tx *txs.AddAddressStateTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) DepositTx(tx *txs.DepositTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (b *backendVisitor) UnlockDepositTx(tx *txs.UnlockDepositTx) error {
	return b.baseTx(&tx.BaseTx)
}

func (s *signerVisitor) AddAddressStateTx(tx *txs.AddAddressStateTx) error {
	txSigners, err := s.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	return sign(s.tx, txSigners)
}

func (s *signerVisitor) DepositTx(tx *txs.DepositTx) error {
	txSigners, err := s.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	return sign(s.tx, txSigners)
}

func (s *signerVisitor) UnlockDepositTx(tx *txs.UnlockDepositTx) error {
	txSigners, err := s.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	return sign(s.tx, txSigners)
}
