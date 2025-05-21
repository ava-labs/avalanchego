// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package issuers

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

type EthClientSimple interface {
	ChainID(ctx context.Context) (*big.Int, error)
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
	SendTransaction(ctx context.Context, tx *types.Transaction) error
}

type IssueTracker interface {
	Issue(tx common.Hash)
}

// Simple generates and issues transactions sending 0 fund to the sender.
type Simple struct {
	// Injected parameters
	client    EthClientSimple
	tracker   IssueTracker
	key       *ecdsa.PrivateKey
	gasFeeCap *big.Int

	// Determined by constructor
	address   common.Address // corresponding to key
	signer    types.Signer
	chainID   *big.Int
	gasTipCap *big.Int

	// State
	nonce uint64
}

func NewSimple(ctx context.Context, client EthClientSimple, tracker IssueTracker,
	nonce uint64, maxFeeCap *big.Int, key *ecdsa.PrivateKey,
) (*Simple, error) {
	address := ethcrypto.PubkeyToAddress(key.PublicKey)

	bigGwei := big.NewInt(params.GWei)
	gasTipCap := new(big.Int).Mul(bigGwei, big.NewInt(1))
	gasFeeCap := new(big.Int).Mul(bigGwei, maxFeeCap)

	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting chain id: %w", err)
	}

	return &Simple{
		client:    client,
		tracker:   tracker,
		key:       key,
		address:   address,
		nonce:     nonce,
		signer:    types.LatestSignerForChainID(chainID),
		chainID:   chainID,
		gasTipCap: gasTipCap,
		gasFeeCap: gasFeeCap,
	}, nil
}

func (s *Simple) GenerateAndIssueTx(ctx context.Context) (common.Hash, error) {
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
		return common.Hash{}, err
	}

	if err := s.client.SendTransaction(ctx, tx); err != nil {
		return common.Hash{}, fmt.Errorf("issuing transaction with nonce %d: %w", s.nonce, err)
	}
	s.nonce++
	txHash := tx.Hash()
	s.tracker.Issue(txHash)
	return txHash, nil
}
