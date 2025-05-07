// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package issuers

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

type EthClientSimple interface {
	ChainID(ctx context.Context) (*big.Int, error)
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
	SendTransaction(ctx context.Context, tx *types.Transaction) error
}

type IssueTracker interface {
	Issue(tx *types.Transaction)
}

// Simple generates and issues transactions sending 0 fund to the sender.
type Simple struct {
	// Injected parameters
	client      EthClientSimple
	tracker     IssueTracker
	key         *ecdsa.PrivateKey
	gasFeeCap   *big.Int
	issuePeriod time.Duration

	// Determined by constructor
	address   common.Address // corresponding to key
	signer    types.Signer
	chainID   *big.Int
	gasTipCap *big.Int

	// State
	nonce     uint64
	lastIssue time.Time
}

func NewSimple(ctx context.Context, client EthClientSimple, tracker IssueTracker,
	maxFeeCap *big.Int, key *ecdsa.PrivateKey, issuePeriod time.Duration,
) (*Simple, error) {
	address := ethcrypto.PubkeyToAddress(key.PublicKey)
	blockNumber := (*big.Int)(nil)
	nonce, err := client.NonceAt(ctx, address, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("getting nonce for address %s: %w", address, err)
	}

	bigGwei := big.NewInt(params.GWei)
	gasTipCap := new(big.Int).Mul(bigGwei, big.NewInt(1))
	gasFeeCap := new(big.Int).Mul(bigGwei, maxFeeCap)

	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting chain id: %w", err)
	}

	return &Simple{
		client:      client,
		tracker:     tracker,
		key:         key,
		address:     address,
		nonce:       nonce,
		signer:      types.LatestSignerForChainID(chainID),
		chainID:     chainID,
		gasTipCap:   gasTipCap,
		gasFeeCap:   gasFeeCap,
		issuePeriod: issuePeriod,
	}, nil
}

func (s *Simple) GenerateAndIssueTx(ctx context.Context) (*types.Transaction, error) {
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

	if s.issuePeriod > 0 && !s.lastIssue.IsZero() &&
		time.Since(s.lastIssue) < s.issuePeriod {
		timer := time.NewTimer(s.issuePeriod - time.Since(s.lastIssue))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}

	if err := s.client.SendTransaction(ctx, tx); err != nil {
		return nil, fmt.Errorf("issuing transaction with nonce %d: %w", s.nonce, err)
	}
	s.nonce++
	s.tracker.Issue(tx)
	s.lastIssue = time.Now()
	return tx, nil
}
