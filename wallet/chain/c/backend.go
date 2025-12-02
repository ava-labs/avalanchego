// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	ethcommon "github.com/ava-labs/libevm/common"
)

var (
	_ Backend = (*backend)(nil)

	errUnknownTxType = errors.New("unknown tx type")
)

// Backend defines the full interface required to support a C-chain wallet.
type Backend interface {
	common.ChainUTXOs
	BuilderBackend
	SignerBackend

	AcceptAtomicTx(ctx context.Context, tx *atomic.Tx) error
}

type backend struct {
	common.ChainUTXOs

	accountsLock sync.RWMutex
	accounts     map[ethcommon.Address]*Account
}

type Account struct {
	Balance *big.Int
	Nonce   uint64
}

func NewBackend(
	utxos common.ChainUTXOs,
	accounts map[ethcommon.Address]*Account,
) Backend {
	return &backend{
		ChainUTXOs: utxos,
		accounts:   accounts,
	}
}

func (b *backend) AcceptAtomicTx(ctx context.Context, tx *atomic.Tx) error {
	switch tx := tx.UnsignedAtomicTx.(type) {
	case *atomic.UnsignedImportTx:
		for _, input := range tx.ImportedInputs {
			utxoID := input.InputID()
			if err := b.RemoveUTXO(ctx, tx.SourceChain, utxoID); err != nil {
				return err
			}
		}

		b.accountsLock.Lock()
		defer b.accountsLock.Unlock()

		for _, output := range tx.Outs {
			account, ok := b.accounts[output.Address]
			if !ok {
				continue
			}

			balance := new(big.Int).SetUint64(output.Amount)
			balance.Mul(balance, avaxConversionRate)
			account.Balance.Add(account.Balance, balance)
		}
	case *atomic.UnsignedExportTx:
		txID := tx.ID()
		for i, out := range tx.ExportedOutputs {
			err := b.AddUTXO(
				ctx,
				tx.DestinationChain,
				&avax.UTXO{
					UTXOID: avax.UTXOID{
						TxID:        txID,
						OutputIndex: uint32(i),
					},
					Asset: avax.Asset{ID: out.AssetID()},
					Out:   out.Out,
				},
			)
			if err != nil {
				return err
			}
		}

		b.accountsLock.Lock()
		defer b.accountsLock.Unlock()

		for _, input := range tx.Ins {
			account, ok := b.accounts[input.Address]
			if !ok {
				continue
			}

			balance := new(big.Int).SetUint64(input.Amount)
			balance.Mul(balance, avaxConversionRate)
			if account.Balance.Cmp(balance) == -1 {
				return errInsufficientFunds
			}
			account.Balance.Sub(account.Balance, balance)

			newNonce, err := math.Add(input.Nonce, 1)
			if err != nil {
				return err
			}
			account.Nonce = newNonce
		}
	default:
		return fmt.Errorf("%w: %T", errUnknownTxType, tx)
	}
	return nil
}

func (b *backend) Balance(_ context.Context, addr ethcommon.Address) (*big.Int, error) {
	b.accountsLock.RLock()
	defer b.accountsLock.RUnlock()

	account, exists := b.accounts[addr]
	if !exists {
		return nil, database.ErrNotFound
	}
	return account.Balance, nil
}

func (b *backend) Nonce(_ context.Context, addr ethcommon.Address) (uint64, error) {
	b.accountsLock.RLock()
	defer b.accountsLock.RUnlock()

	account, exists := b.accounts[addr]
	if !exists {
		return 0, database.ErrNotFound
	}
	return account.Nonce, nil
}
