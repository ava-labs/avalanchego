// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package generate

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"maps"
	"math/big"

	"github.com/ava-labs/avalanchego/tests/load/agent"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/params"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

var _ agent.TxGenerator[*types.Transaction] = (*Distributor)(nil)

// Distributor
type Distributor struct {
	from      *ecdsa.PrivateKey
	address   common.Address // corresponding to `from`
	nonce     uint64
	to        map[*ecdsa.PrivateKey]*big.Int
	signer    types.Signer
	chainID   *big.Int
	gasTipCap *big.Int
	gasFeeCap *big.Int
}

func NewDistributor(
	ctx context.Context,
	client *ethclient.Client,
	from *ecdsa.PrivateKey,
	to map[*ecdsa.PrivateKey]*big.Int,
) (*Distributor, error) {
	address := ethcrypto.PubkeyToAddress(from.PublicKey)
	blockNumber := (*big.Int)(nil)
	nonce, err := client.NonceAt(ctx, address, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("getting nonce for address %s: %w", address, err)
	}

	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting chain ID: %w", err)
	}

	gasTipCap, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting suggested gas tip cap: %w", err)
	}

	gasFeeCap, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting suggested gas price: %w", err)
	}
	gasFeeCap = gasFeeCap.Mul(gasFeeCap, big.NewInt(2)) // double the gas price to ensure the transaction is mined

	return &Distributor{
		from:      from,
		address:   address,
		nonce:     nonce,
		to:        maps.Clone(to), // need to clone the map to avoid modifying the original
		signer:    types.LatestSignerForChainID(chainID),
		chainID:   chainID,
		gasTipCap: gasTipCap,
		gasFeeCap: gasFeeCap,
	}, nil
}

func (d *Distributor) GenerateTx() (*types.Transaction, error) {
	if len(d.to) == 0 {
		// The caller of this function should prevent this error from being
		// returned by bounding the number of transactions generated.
		return nil, errors.New("no more keys to distribute funds to")
	}

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
	d.nonce++
	return tx, nil
}
