// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
	ethparams "github.com/ava-labs/libevm/params"
	"github.com/ava-labs/subnet-evm/cmd/simulator/key"
	"github.com/ava-labs/subnet-evm/cmd/simulator/metrics"
	"github.com/ava-labs/subnet-evm/cmd/simulator/txs"
	"github.com/ava-labs/subnet-evm/ethclient"
)

// DistributeFunds ensures that each address in keys has at least [minFundsPerAddr] by sending funds
// from the key with the highest starting balance.
// This function returns a set of at least [numKeys] keys, each having a minimum balance [minFundsPerAddr].
func DistributeFunds(ctx context.Context, client ethclient.Client, keys []*key.Key, numKeys int, minFundsPerAddr *big.Int, m *metrics.Metrics) ([]*key.Key, error) {
	if len(keys) < numKeys {
		return nil, fmt.Errorf("insufficient number of keys %d < %d", len(keys), numKeys)
	}
	fundedKeys := make([]*key.Key, 0, numKeys)
	// TODO: clean up fund distribution.
	needFundsKeys := make([]*key.Key, 0)
	needFundsAddrs := make([]common.Address, 0)

	maxFundsKey := keys[0]
	maxFundsBalance := common.Big0
	log.Info("Checking balance of each key to distribute funds")
	for _, key := range keys {
		balance, err := client.BalanceAt(ctx, key.Address, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch balance for addr %s: %w", key.Address, err)
		}

		if balance.Cmp(minFundsPerAddr) < 0 {
			needFundsKeys = append(needFundsKeys, key)
			needFundsAddrs = append(needFundsAddrs, key.Address)
		} else {
			fundedKeys = append(fundedKeys, key)
		}

		if balance.Cmp(maxFundsBalance) > 0 {
			maxFundsKey = key
			maxFundsBalance = balance
		}
	}
	requiredFunds := new(big.Int).Mul(minFundsPerAddr, big.NewInt(int64(numKeys)))
	if maxFundsBalance.Cmp(requiredFunds) < 0 {
		return nil, fmt.Errorf("insufficient funds to distribute %d < %d", maxFundsBalance, requiredFunds)
	}
	log.Info("Found max funded key", "address", maxFundsKey.Address, "balance", maxFundsBalance, "numFundAddrs", len(needFundsAddrs))
	if len(fundedKeys) >= numKeys {
		return fundedKeys[:numKeys], nil
	}

	// If there are not enough funded keys, cut [needFundsAddrs] to the number of keys that
	// must be funded to reach [numKeys] required.
	fundKeysCutLen := numKeys - len(fundedKeys)
	needFundsKeys = needFundsKeys[:fundKeysCutLen]
	needFundsAddrs = needFundsAddrs[:fundKeysCutLen]

	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chainID: %w", err)
	}
	gasFeeCap, err := client.EstimateBaseFee(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch estimated base fee: %w", err)
	}
	gasTipCap, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch suggested gas tip: %w", err)
	}
	signer := types.LatestSignerForChainID(chainID)

	// Generate a sequence of transactions to distribute the required funds.
	log.Info("Generating distribution transactions...")
	i := 0
	txGenerator := func(key *ecdsa.PrivateKey, nonce uint64) (*types.Transaction, error) {
		tx, err := types.SignNewTx(key, signer, &types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			GasTipCap: gasTipCap,
			GasFeeCap: gasFeeCap,
			Gas:       ethparams.TxGas,
			To:        &needFundsAddrs[i],
			Data:      nil,
			Value:     requiredFunds,
		})
		if err != nil {
			return nil, err
		}
		i++
		return tx, nil
	}

	numTxs := uint64(len(needFundsAddrs))
	txSequence, err := txs.GenerateTxSequence(ctx, txGenerator, client, maxFundsKey.PrivKey, numTxs, false)
	if err != nil {
		return nil, fmt.Errorf("failed to generate fund distribution sequence from %s of length %d", maxFundsKey.Address, len(needFundsAddrs))
	}
	worker := NewSingleAddressTxWorker(ctx, client, maxFundsKey.Address)
	txFunderAgent := txs.NewIssueNAgent[*types.Transaction](txSequence, worker, numTxs, m)

	if err := txFunderAgent.Execute(ctx); err != nil {
		return nil, err
	}
	for _, addr := range needFundsAddrs {
		balance, err := client.BalanceAt(ctx, addr, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch balance for addr %s: %w", addr, err)
		}
		log.Info("Funded address has balance", "addr", addr, "balance", balance)
	}
	fundedKeys = append(fundedKeys, needFundsKeys...)
	return fundedKeys, nil
}
