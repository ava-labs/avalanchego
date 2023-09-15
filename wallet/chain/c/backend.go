// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	stdcontext "context"

	"github.com/ava-labs/coreth/plugin/evm"

	ethcommon "github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
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

	AcceptAtomicTx(ctx stdcontext.Context, tx *evm.Tx) error
}

type backend struct {
	Context
	common.ChainUTXOs

	accountsLock sync.RWMutex
	accounts     map[ethcommon.Address]*Account
}

type Account struct {
	Balance *big.Int
	Nonce   uint64
}

func NewBackend(
	ctx Context,
	utxos common.ChainUTXOs,
	accounts map[ethcommon.Address]*Account,
) Backend {
	return &backend{
		Context:    ctx,
		ChainUTXOs: utxos,
		accounts:   accounts,
	}
}

func (b *backend) AcceptAtomicTx(ctx stdcontext.Context, tx *evm.Tx) error {
	switch tx := tx.UnsignedAtomicTx.(type) {
	case *evm.UnsignedImportTx:
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
	case *evm.UnsignedExportTx:
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

			newNonce, err := math.Add64(input.Nonce, 1)
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

func (b *backend) Balance(_ stdcontext.Context, addr ethcommon.Address) (*big.Int, error) {
	b.accountsLock.RLock()
	defer b.accountsLock.RUnlock()

	account, exists := b.accounts[addr]
	if !exists {
		return nil, database.ErrNotFound
	}
	return account.Balance, nil
}

func (b *backend) Nonce(_ stdcontext.Context, addr ethcommon.Address) (uint64, error) {
	b.accountsLock.RLock()
	defer b.accountsLock.RUnlock()

	account, exists := b.accounts[addr]
	if !exists {
		return 0, database.ErrNotFound
	}
	return account.Nonce, nil
}
