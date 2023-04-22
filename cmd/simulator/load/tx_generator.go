// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"crypto/ecdsa"
	"fmt"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

type CreateTx func(key *ecdsa.PrivateKey, nonce uint64) (*types.Transaction, error)

// GenerateTxSequence fetches the current nonce of key and calls [generator] [numTxs] times sequentially to generate a sequence of transactions.
func GenerateTxSequence(ctx context.Context, generator CreateTx, client ethclient.Client, key *ecdsa.PrivateKey, numTxs uint64) ([]*types.Transaction, error) {
	address := ethcrypto.PubkeyToAddress(key.PublicKey)
	startingNonce, err := client.NonceAt(ctx, address, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch nonce for address %s: %w", address, err)
	}
	txs := make([]*types.Transaction, 0, numTxs)
	for i := uint64(0); i < numTxs; i++ {
		tx, err := generator(key, startingNonce+i)
		if err != nil {
			return nil, fmt.Errorf("failed to sign tx at index %d: %w", i, err)
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

func GenerateTxSequences(ctx context.Context, generator CreateTx, client ethclient.Client, keys []*ecdsa.PrivateKey, txsPerKey uint64) ([][]*types.Transaction, error) {
	txSequences := make([][]*types.Transaction, len(keys))
	for i, key := range keys {
		txs, err := GenerateTxSequence(ctx, generator, client, key, txsPerKey)
		if err != nil {
			return nil, fmt.Errorf("failed to generate tx sequence at index %d: %w", i, err)
		}
		txSequences[i] = txs
	}
	return txSequences, nil
}
