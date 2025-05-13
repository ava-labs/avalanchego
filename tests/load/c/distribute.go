// Co// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sync"

	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

// distribute distributes as close to equally the funds for each given key.
// It is not exact because of the gas fees for each transaction, so a minimum
// balance required for each key is calculated as follows:
// minBalance = averageBalance - (numberOfKeys * txCost)
func distribute(ctx context.Context, endpoint string, keys []*ecdsa.PrivateKey) error {
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		return fmt.Errorf("dialing %s: %w", endpoint, err)
	}

	gasFeeCap, err := client.EstimateBaseFee(ctx)
	if err != nil {
		return fmt.Errorf("getting estimated base fee: %w", err)
	}
	txCost := big.NewInt(params.GWei)
	txCost.Mul(txCost, gasFeeCap)
	txCost.Mul(txCost, new(big.Int).SetUint64(params.TxGas))

	keyToBalance, err := getKeyToBalance(ctx, client, keys)
	if err != nil {
		return fmt.Errorf("getting key-balance pairs: %w", err)
	}

	minBalance := determineMinBalance(keyToBalance, txCost)

	txs, err := createTxs(ctx, client, keyToBalance, minBalance, txCost, gasFeeCap)
	if err != nil {
		return fmt.Errorf("creating transactions: %w", err)
	} else if len(txs) == 0 {
		return nil
	}

	collector := newAcceptedTxsCollector(client)
	err = collector.start(ctx)
	if err != nil {
		return fmt.Errorf("subscribing to accepted transactions: %w", err)
	}

	for _, tx := range txs {
		if err := client.SendTransaction(ctx, tx); err != nil {
			return fmt.Errorf("sending transaction: %w", err)
		}
		collector.issued(tx.Hash())
	}

	if err := collector.wait(ctx); err != nil {
		return fmt.Errorf("waiting for accepted transactions: %w", err)
	}

	err = checkBalances(ctx, client, keys, minBalance)
	if err != nil {
		return fmt.Errorf("checking balances after funding: %w", err)
	}

	return nil
}

func getKeyToBalance(ctx context.Context, client ethclient.Client, keys []*ecdsa.PrivateKey) (map[*ecdsa.PrivateKey]*big.Int, error) {
	keyToBalance := make(map[*ecdsa.PrivateKey]*big.Int, len(keys))
	for _, key := range keys {
		address := ethcrypto.PubkeyToAddress(key.PublicKey)
		blockNumber := (*big.Int)(nil)
		balance, err := client.BalanceAt(ctx, address, blockNumber)
		if err != nil {
			return nil, fmt.Errorf("fetching balance for address %s: %w", address, err)
		}
		keyToBalance[key] = balance
	}

	return keyToBalance, nil
}

func determineMinBalance(keyToBalance map[*ecdsa.PrivateKey]*big.Int, txCost *big.Int) *big.Int {
	totalBalance := new(big.Int)
	for _, balance := range keyToBalance {
		totalBalance.Add(totalBalance, balance)
	}
	averageBalance := new(big.Int).Div(totalBalance, big.NewInt(int64(len(keyToBalance))))
	maxTxCosts := new(big.Int).Mul(txCost, big.NewInt(int64(len(keyToBalance))))
	return averageBalance.Sub(averageBalance, maxTxCosts)
}

func createTxs(ctx context.Context, client ethclient.Client, keyToBalance map[*ecdsa.PrivateKey]*big.Int, minBalance, txCost, gasFeeCap *big.Int) ([]*types.Transaction, error) {
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting chain id: %w", err)
	}

	var txs []*types.Transaction
	for toKey, balance := range keyToBalance {
		diff := new(big.Int).Sub(balance, minBalance)
		if diff.Cmp(common.Big0) >= 0 {
			continue // this key has enough funds
		}
		diff = new(big.Int).Neg(diff)
		to := ethcrypto.PubkeyToAddress(toKey.PublicKey)
		remainingToMove := new(big.Int).Set(diff)
		for fromKey, fromBalance := range keyToBalance {
			if fromKey == toKey {
				continue
			}
			available := new(big.Int).Sub(fromBalance, minBalance)
			available.Sub(available, txCost)
			if available.Cmp(common.Big0) <= 0 {
				continue // this key has not enough funds
			}
			toMove := diff
			if available.Cmp(diff) < 0 {
				// not enough funds to send diff entirely
				toMove = available
			}
			remainingToMove.Sub(remainingToMove, toMove)
			fromBalance.Sub(fromBalance, toMove)

			fromAddr := ethcrypto.PubkeyToAddress(fromKey.PublicKey)
			blockNumber := (*big.Int)(nil)
			nonce, err := client.NonceAt(ctx, fromAddr, blockNumber)
			if err != nil {
				return nil, fmt.Errorf("getting nonce for address %s: %w", fromAddr, err)
			}

			signer := types.LatestSignerForChainID(chainID)
			tx, err := types.SignNewTx(fromKey, signer, &types.DynamicFeeTx{
				ChainID:   chainID,
				Nonce:     nonce,
				GasTipCap: big.NewInt(0),
				GasFeeCap: gasFeeCap,
				Gas:       params.TxGas,
				To:        &to,
				Data:      nil,
				Value:     toMove,
			})
			if err != nil {
				return nil, fmt.Errorf("signing transaction: %w", err)
			}
			txs = append(txs, tx)

			if remainingToMove.Cmp(common.Big0) == 0 {
				break
			}
		}
	}
	return txs, nil
}

func checkBalances(ctx context.Context, client ethclient.Client, keys []*ecdsa.PrivateKey, minBalance *big.Int) error {
	keyToBalance, err := getKeyToBalance(ctx, client, keys)
	if err != nil {
		return fmt.Errorf("getting key to balance for newly funded keys: %w", err)
	}
	for key, balance := range keyToBalance {
		if balance.Cmp(minBalance) < 0 {
			address := ethcrypto.PubkeyToAddress(key.PublicKey)
			return fmt.Errorf("address %s has insufficient funds %d < %d", address, balance, minBalance)
		}
	}
	return nil
}

type acceptedTxsCollector struct {
	// Injected parameters
	client ethclient.Client

	// State
	lock      sync.RWMutex
	inFlight  map[common.Hash]struct{}
	allIssued bool
	waitErr   <-chan error
}

func newAcceptedTxsCollector(client ethclient.Client) *acceptedTxsCollector {
	return &acceptedTxsCollector{
		client:   client,
		inFlight: make(map[common.Hash]struct{}),
	}
}

// start subscribes to the new accepted transactions and
// collects until either the context is done, an error occurs, or all
// transactions are issued and accepted.
func (c *acceptedTxsCollector) start(ctx context.Context) error {
	newAcceptedTxsCh := make(chan *common.Hash)
	subscription, err := c.client.SubscribeNewAcceptedTransactions(ctx, newAcceptedTxsCh)
	if err != nil {
		return err
	}

	ready := make(chan struct{})
	waitErr := make(chan error)
	c.waitErr = waitErr
	go func() {
		defer subscription.Unsubscribe()
		close(ready)
		for {
			select {
			case hash := <-newAcceptedTxsCh:
				if hash == nil {
					continue
				}
				c.lock.Lock()
				delete(c.inFlight, *hash)
				if c.allIssued && len(c.inFlight) == 0 {
					waitErr <- nil
					c.lock.Unlock()
					return
				}
				c.lock.Unlock()
			case <-ctx.Done():
				waitErr <- ctx.Err()
				return
			case err := <-subscription.Err():
				waitErr <- err
				return
			}
		}
	}()
	<-ready

	return nil
}

func (c *acceptedTxsCollector) issued(hash common.Hash) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.inFlight[hash] = struct{}{}
}

// wait must only be called after all transactions are issued.
func (c *acceptedTxsCollector) wait(ctx context.Context) error {
	c.lock.Lock()
	c.allIssued = true
	c.lock.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-c.waitErr:
		return err
	}
}
