// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package issuers

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"maps"
	"math/big"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

type EthClientDistributor interface {
	EthClientSimple
	EstimateBaseFee(ctx context.Context) (*big.Int, error)
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)
}

// Distributor issues transactions to a node to distribute funds from a single address
// to multiple addresses.
type Distributor struct {
	// Injected parameters
	client EthClientDistributor
	from   *ecdsa.PrivateKey
	to     map[*ecdsa.PrivateKey]*big.Int

	// Determined by constructor
	address   common.Address // corresponding to key
	signer    types.Signer
	chainID   *big.Int
	gasFeeCap *big.Int
	gasTipCap *big.Int

	// State
	nonce uint64
}

func NewDistributor(ctx context.Context, client EthClientDistributor,
	from *ecdsa.PrivateKey, to map[*ecdsa.PrivateKey]*big.Int,
) (*Distributor, error) {
	address := ethcrypto.PubkeyToAddress(from.PublicKey)
	blockNumber := (*big.Int)(nil)
	nonce, err := client.NonceAt(ctx, address, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("getting nonce for address %s: %w", address, err)
	}

	gasTipCap, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting suggested gas tip cap: %w", err)
	}

	gasFeeCap, err := client.EstimateBaseFee(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting estimated base fee: %w", err)
	}

	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting chain id: %w", err)
	}

	return &Distributor{
		client:    client,
		from:      from,
		address:   address,
		nonce:     nonce,
		to:        maps.Clone(to), // need to clone the map to avoid modifying the original
		signer:    types.LatestSignerForChainID(chainID),
		chainID:   chainID,
		gasFeeCap: gasFeeCap,
		gasTipCap: gasTipCap,
	}, nil
}

func (d *Distributor) GenerateAndIssueTx(ctx context.Context) (*types.Transaction, error) {
	var toKey *ecdsa.PrivateKey
	var funds *big.Int
	for toKey, funds = range d.to {
		break
	}
	delete(d.to, toKey)

	to := ethcrypto.PubkeyToAddress(toKey.PublicKey)
	tx, err := types.SignNewTx(d.from, d.signer, &types.DynamicFeeTx{
		ChainID:   d.chainID,
		Nonce:     d.nonce,
		GasTipCap: d.gasTipCap,
		GasFeeCap: d.gasFeeCap,
		Gas:       params.TxGas,
		To:        &to,
		Data:      nil,
		Value:     funds,
	})
	if err != nil {
		return nil, err
	}

	if err := d.client.SendTransaction(ctx, tx); err != nil {
		return nil, fmt.Errorf("issuing transaction with nonce %d: %w", d.nonce, err)
	}
	d.nonce++
	return tx, nil
}
