// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"crypto/ecdsa"
	"fmt"

	"github.com/ava-labs/libevm/core/types"
	ethcrypto "github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/subnet-evm/ethclient"
)

var _ TxSequence[*types.Transaction] = (*txSequence)(nil)

type CreateTx func(key *ecdsa.PrivateKey, nonce uint64) (*types.Transaction, error)

func GenerateTxSequence(ctx context.Context, generator CreateTx, client ethclient.Client, key *ecdsa.PrivateKey, numTxs uint64, async bool) (TxSequence[*types.Transaction], error) {
	sequence := &txSequence{
		txChan: make(chan *types.Transaction, numTxs),
	}

	if async {
		go func() {
			defer close(sequence.txChan)

			if err := addTxs(ctx, sequence, generator, client, key, numTxs); err != nil {
				panic(err)
			}
		}()
	} else {
		if err := addTxs(ctx, sequence, generator, client, key, numTxs); err != nil {
			return nil, err
		}
		close(sequence.txChan)
	}

	return sequence, nil
}

func GenerateTxSequences(ctx context.Context, generator CreateTx, client ethclient.Client, keys []*ecdsa.PrivateKey, txsPerKey uint64, async bool) ([]TxSequence[*types.Transaction], error) {
	txSequences := make([]TxSequence[*types.Transaction], len(keys))
	for i, key := range keys {
		txs, err := GenerateTxSequence(ctx, generator, client, key, txsPerKey, async)
		if err != nil {
			return nil, fmt.Errorf("failed to generate tx sequence at index %d: %w", i, err)
		}
		txSequences[i] = txs
	}
	return txSequences, nil
}

func addTxs(ctx context.Context, txSequence *txSequence, generator CreateTx, client ethclient.Client, key *ecdsa.PrivateKey, numTxs uint64) error {
	address := ethcrypto.PubkeyToAddress(key.PublicKey)
	startingNonce, err := client.NonceAt(ctx, address, nil)
	if err != nil {
		return err
	}
	for i := uint64(0); i < numTxs; i++ {
		tx, err := generator(key, startingNonce+i)
		if err != nil {
			return err
		}
		txSequence.txChan <- tx
	}

	return nil
}

type txSequence struct {
	txChan chan *types.Transaction
}

func ConvertTxSliceToSequence(txs []*types.Transaction) TxSequence[*types.Transaction] {
	txChan := make(chan *types.Transaction, len(txs))
	for _, tx := range txs {
		txChan <- tx
	}
	close(txChan)

	return &txSequence{
		txChan: txChan,
	}
}

func (t *txSequence) Chan() <-chan *types.Transaction {
	return t.txChan
}
