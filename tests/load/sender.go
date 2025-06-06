// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"

	ethereum "github.com/ava-labs/libevm"
)

type Builder interface {
	// Create a valid transaction
	BuildTx() (*types.Transaction, error)
}

type Sender struct {
	client *ethclient.Client
}

func NewSender(client *ethclient.Client) *Sender {
	return &Sender{client: client}
}

func (s *Sender) SendTx(
	ctx context.Context,
	builder Builder,
	pingFrequency time.Duration,
	issuanceHandler func(time.Duration),
	confirmationHandler func(*types.Receipt, time.Duration),
) error {
	startTime := time.Now()
	tx, err := builder.BuildTx()
	if err != nil {
		return err
	}

	if err := s.client.SendTransaction(ctx, tx); err != nil {
		return err
	}

	issuanceDuration := time.Since(startTime)
	issuanceHandler(issuanceDuration)

	receipt, err := awaitTx(ctx, s.client, tx.Hash(), pingFrequency)
	if err != nil {
		return err
	}

	totalDuration := time.Since(startTime)
	confirmationDuration := totalDuration - issuanceDuration
	confirmationHandler(receipt, confirmationDuration)

	return nil
}

func awaitTx(
	ctx context.Context,
	client *ethclient.Client,
	txHash common.Hash,
	pingFrequency time.Duration,
) (*types.Receipt, error) {
	ticker := time.NewTicker(pingFrequency)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}

		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err != nil {
			if errors.Is(err, ethereum.NotFound) {
				continue
			}

			return nil, err
		}

		if receipt.Status != 1 {
			return nil, fmt.Errorf("failed tx: %d", receipt.Status)
		}

		return receipt, nil
	}
}
