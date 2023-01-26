// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/params"

	"github.com/ethereum/go-ethereum/common"
)

type EvmClient struct {
	rpcEp string

	ethClient ethclient.Client
	chainID   *big.Int
	signer    types.Signer

	feeCap      *big.Int
	priorityFee *big.Int
}

func NewEvmClient(ep string, baseFee uint64, priorityFee uint64) (*EvmClient, error) {
	ethCli, err := ethclient.Dial(ep)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	chainID, err := ethCli.ChainID(ctx)
	if err != nil {
		return nil, err
	}

	pFee := new(big.Int).SetUint64(priorityFee * params.GWei)
	feeCap := new(big.Int).Add(new(big.Int).SetUint64(baseFee*params.GWei), pFee)

	return &EvmClient{
		rpcEp: ep,

		ethClient: ethCli,
		chainID:   chainID,
		signer:    types.LatestSignerForChainID(chainID),

		feeCap:      feeCap,
		priorityFee: pFee,
	}, nil
}

func (ec *EvmClient) FetchBalance(ctx context.Context, addr common.Address) (*big.Int, error) {
	for ctx.Err() == nil {
		balance, err := ec.ethClient.BalanceAt(ctx, addr, nil)
		if err != nil {
			log.Printf("could not get balance: %s", err.Error())
			time.Sleep(time.Second)
			continue
		}
		return balance, nil
	}
	return nil, ctx.Err()
}

func (ec *EvmClient) FetchNonce(ctx context.Context, addr common.Address) (uint64, error) {
	for ctx.Err() == nil {
		nonce, err := ec.ethClient.NonceAt(ctx, addr, nil)
		if err != nil {
			log.Printf("could not get nonce: %s", err.Error())
			time.Sleep(time.Second)
			continue
		}
		return nonce, nil
	}
	return 0, ctx.Err()
}

func (ec *EvmClient) WaitForBalance(ctx context.Context, addr common.Address, minBalance *big.Int) error {
	for ctx.Err() == nil {
		bal, err := ec.FetchBalance(ctx, addr)
		if err != nil {
			log.Printf("could not get balance: %s", err.Error())
			time.Sleep(time.Second)
			continue
		}

		if bal.Cmp(minBalance) >= 0 {
			log.Printf("found balance of %s", bal.String())
			return nil
		}

		log.Printf("waiting for balance of %s on %s", minBalance.String(), addr.Hex())
		time.Sleep(5 * time.Second)
	}

	return ctx.Err()
}

func (ec *EvmClient) ConfirmTx(ctx context.Context, txHash common.Hash) (*big.Int, error) {
	for ctx.Err() == nil {
		result, pending, _ := ec.ethClient.TransactionByHash(ctx, txHash)
		if result == nil || pending {
			time.Sleep(time.Second)
			continue
		}
		// XXX: this uses gas instead of gas used, so it may be incorrect if the transaction does more than a simple transfer
		return result.Cost(), nil
	}
	return nil, ctx.Err()
}

// makes transfer tx and returns the new balance of sender
func (ec *EvmClient) TransferTx(
	ctx context.Context,
	sender common.Address,
	senderPriv *ecdsa.PrivateKey,
	recipient common.Address,
	transferAmount *big.Int) (*big.Int, error) {
	for ctx.Err() == nil {
		senderBal, err := ec.FetchBalance(ctx, sender)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch balance: %w", err)
		}

		if senderBal.Cmp(transferAmount) < 0 {
			return nil, fmt.Errorf("not enough balance %s to transfer %s", senderBal, transferAmount)
		}

		nonce, err := ec.FetchNonce(ctx, sender)
		if err != nil {
			return nil, err
		}

		signedTx, err := types.SignTx(
			types.NewTx(&types.DynamicFeeTx{
				ChainID:   ec.chainID,
				Nonce:     nonce,
				To:        &recipient,
				Gas:       uint64(21000),
				GasFeeCap: ec.feeCap,
				GasTipCap: ec.priorityFee,
				Value:     transferAmount,
			}),
			ec.signer,
			senderPriv,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to sign transaction: %w", err)
		}

		if err := ec.ethClient.SendTransaction(ctx, signedTx); err != nil {
			log.Printf("failed to send transaction: %v (key address %s)", err, sender)
			return nil, err
		}

		txHash := signedTx.Hash()
		cost, err := ec.ConfirmTx(ctx, txHash)
		if err != nil {
			return nil, err
		}

		senderBal = new(big.Int).Sub(senderBal, cost)
		senderBal = new(big.Int).Sub(senderBal, transferAmount)
		return senderBal, nil
	}

	return nil, ctx.Err()
}
