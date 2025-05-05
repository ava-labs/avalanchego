// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"sort"
	"time"

	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/tests/load/agent"
	"github.com/ava-labs/avalanchego/tests/load/generate"
	"github.com/ava-labs/avalanchego/tests/load/issue"
	"github.com/ava-labs/avalanchego/tests/load/listen"
	"github.com/ava-labs/avalanchego/tests/load/orchestrate"
	"github.com/ava-labs/avalanchego/tests/load/tracker"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

// ensureMinimumFunds ensures that each key has at least `minFundsPerAddr` by sending funds
// from the key with the highest starting balance to keys with balances below the minimum.
func ensureMinimumFunds(ctx context.Context, endpoint string, keys []*ecdsa.PrivateKey, minFundsPerAddr *big.Int) error {
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		return fmt.Errorf("dialing %s: %w", endpoint, err)
	}

	keyToBalance, err := getKeyToBalance(ctx, client, keys)
	if err != nil {
		return fmt.Errorf("getting key to balance: %w", err)
	}

	sort.Slice(keys, func(i, j int) bool {
		return keyToBalance[keys[i]].Cmp(keyToBalance[keys[j]]) < 0
	})

	needFundsKeys := make(map[*ecdsa.PrivateKey]*big.Int, len(keys))
	totalFundsRequired := new(big.Int)
	for _, key := range keys {
		balance := keyToBalance[key]
		diff := new(big.Int).Sub(minFundsPerAddr, balance)
		if diff.Cmp(common.Big0) <= 0 {
			// Found the key with funds equal or above the minimum funds,
			// so all the next keys have enough funds.
			break
		}
		totalFundsRequired.Add(totalFundsRequired, diff)
		needFundsKeys[key] = diff
	}
	totalFundsRequired.Add(totalFundsRequired, minFundsPerAddr) // for largest balance key

	if len(needFundsKeys) == 0 {
		return nil
	}

	maxFundsKey := keys[len(keys)-1]
	maxFundsBalance := keyToBalance[maxFundsKey]
	if maxFundsBalance.Cmp(totalFundsRequired) < 0 {
		return fmt.Errorf("insufficient funds %d (require %d) to distribute to %d keys",
			maxFundsBalance, totalFundsRequired, len(needFundsKeys))
	}

	maxFundsAddress := ethcrypto.PubkeyToAddress(maxFundsKey.PublicKey)
	txTarget := uint64(len(needFundsKeys))
	generator, err := generate.NewDistributor(ctx, client, maxFundsKey, needFundsKeys)
	if err != nil {
		return fmt.Errorf("creating distribution generator: %w", err)
	}
	tracker := tracker.NewNoop()
	const issuePeriod = 0 // no delay between transaction issuance
	issuer := issue.New(client, tracker, issuePeriod)
	listener := listen.New(client, tracker, txTarget, maxFundsAddress)
	agents := []*agent.Agent[*types.Transaction, common.Hash]{
		agent.New(txTarget, generator, issuer, listener, tracker),
	}
	orchestrator := orchestrate.NewBurstOrchestrator(agents, time.Second)

	err = orchestrator.Execute(ctx)
	if err != nil {
		return fmt.Errorf("executing fund distribution transactions: %w", err)
	}

	// Wait for transactions to be taken into account, especially because
	// the orchestrator Execute method does not wait for all transactions
	// to be confirmed.
	const timeout = 3 * time.Second
	timer := time.NewTimer(timeout)
	select {
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	case <-timer.C:
	}

	err = checkBalancesHaveMin(ctx, client, slices.Collect(maps.Keys(needFundsKeys)), minFundsPerAddr)
	if err != nil {
		return fmt.Errorf("checking balances after funding: %w", err)
	}

	return nil
}

type ethClientBalancer interface {
	BalanceAt(ctx context.Context, address common.Address, blockNumber *big.Int) (*big.Int, error)
}

func getKeyToBalance(ctx context.Context, client ethClientBalancer, keys []*ecdsa.PrivateKey) (map[*ecdsa.PrivateKey]*big.Int, error) {
	keyToBalance := make(map[*ecdsa.PrivateKey]*big.Int, len(keys))
	var err error
	for _, key := range keys {
		address := ethcrypto.PubkeyToAddress(key.PublicKey)
		blockNumber := (*big.Int)(nil)
		keyToBalance[key], err = client.BalanceAt(ctx, address, blockNumber)
		if err != nil {
			return nil, fmt.Errorf("fetching balance for address %s: %w", address, err)
		}
	}
	return keyToBalance, nil
}

func checkBalancesHaveMin(ctx context.Context, client ethClientBalancer, keys []*ecdsa.PrivateKey, minFundsPerAddr *big.Int) error {
	keyToBalance, err := getKeyToBalance(ctx, client, keys)
	if err != nil {
		return fmt.Errorf("getting key to balance for newly funded keys: %w", err)
	}
	for key, balance := range keyToBalance {
		if balance.Cmp(minFundsPerAddr) < 0 {
			address := ethcrypto.PubkeyToAddress(key.PublicKey)
			return fmt.Errorf("address %s has insufficient funds %d < %d", address, balance, minFundsPerAddr)
		}
	}
	return nil
}

// end goal for time per block

// Opcode distribution - debug trace block endpoint
// Solidity contract with opcode input read/write to different addresses (new) in storage, or application level contracts like uniswap, erc20
