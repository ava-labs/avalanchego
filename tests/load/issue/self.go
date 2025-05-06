// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package issue

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

type EthClient interface {
	ChainID(ctx context.Context) (*big.Int, error)
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
	SendTransaction(ctx context.Context, tx *types.Transaction) error
}

type Tracker interface {
	Issue(txHash common.Hash)
}

// Self generates and issues transactions sending 0 fund to the sender.
type Self struct {
	// Injected parameters
	client      EthClient
	tracker     Tracker
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

func NewSelf(ctx context.Context, client EthClient, tracker Tracker,
	maxFeeCap *big.Int, key *ecdsa.PrivateKey, issuePeriod time.Duration,
) (*Self, error) {
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

	return &Self{
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

func (i *Self) GenerateAndIssueTx(ctx context.Context) (*types.Transaction, error) {
	tx, err := types.SignNewTx(i.key, i.signer, &types.DynamicFeeTx{
		ChainID:   i.chainID,
		Nonce:     i.nonce,
		GasTipCap: i.gasTipCap,
		GasFeeCap: i.gasFeeCap,
		Gas:       params.TxGas,
		To:        &i.address, // self
		Data:      nil,
		Value:     common.Big0,
	})
	if err != nil {
		return nil, err
	}

	if i.issuePeriod > 0 && !i.lastIssue.IsZero() &&
		time.Since(i.lastIssue) < i.issuePeriod {
		timer := time.NewTimer(i.issuePeriod - time.Since(i.lastIssue))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}

	if err := i.client.SendTransaction(ctx, tx); err != nil {
		return nil, fmt.Errorf("issuing transaction with nonce %d: %w", i.nonce, err)
	}
	i.nonce++
	i.tracker.Issue(tx.Hash())
	i.lastIssue = time.Now()
	return tx, nil
}
