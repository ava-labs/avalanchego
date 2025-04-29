// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package generator

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

type SelfClient interface {
	ChainID(ctx context.Context) (*big.Int, error)
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
}

// Self generates transactions sending 0 fund to the sender key.
type Self struct {
	key       *ecdsa.PrivateKey
	address   common.Address // corresponding to key
	nonce     uint64
	signer    types.Signer
	chainID   *big.Int
	gasTipCap *big.Int
	gasFeeCap *big.Int
}

func NewSelf(ctx context.Context, client SelfClient,
	maxTipCap, maxFeeCap *big.Int, key *ecdsa.PrivateKey,
) (*Self, error) {
	address := ethcrypto.PubkeyToAddress(key.PublicKey)
	blockNumber := (*big.Int)(nil)
	nonce, err := client.NonceAt(ctx, address, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("getting nonce for address %s: %w", address, err)
	}

	bigGwei := big.NewInt(params.GWei)
	gasTipCap := new(big.Int).Mul(bigGwei, maxTipCap)
	gasFeeCap := new(big.Int).Mul(bigGwei, maxFeeCap)
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting chain id: %w", err)
	}

	return &Self{
		key:       key,
		address:   address,
		nonce:     nonce,
		signer:    types.LatestSignerForChainID(chainID),
		chainID:   chainID,
		gasTipCap: gasTipCap,
		gasFeeCap: gasFeeCap,
	}, nil
}

func (s *Self) GenerateTx() (*types.Transaction, error) {
	tx, err := types.SignNewTx(s.key, s.signer, &types.DynamicFeeTx{
		ChainID:   s.chainID,
		Nonce:     s.nonce,
		GasTipCap: s.gasTipCap,
		GasFeeCap: s.gasFeeCap,
		Gas:       params.TxGas,
		To:        &s.address, // self
		Data:      nil,
		Value:     common.Big0,
	})
	if err != nil {
		return nil, err
	}
	s.nonce++
	return tx, nil
}
