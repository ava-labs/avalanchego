// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package worker

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"math/rand"
	"time"

	"github.com/ava-labs/subnet-evm/cmd/simulator/key"
	"github.com/ava-labs/subnet-evm/cmd/simulator/metrics"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"golang.org/x/sync/errgroup"
)

var (
	transferGasLimit = uint64(21000)
	transferAmount   = big.NewInt(1)
	workDelay        = time.Duration(100 * time.Millisecond)
	retryDelay       = time.Duration(500 * time.Millisecond)

	chainID     *big.Int
	signer      types.Signer
	feeCap      *big.Int
	priorityFee *big.Int

	maxTransferCost  *big.Int
	requestAmount    *big.Int
	minFunderBalance *big.Int
)

func setupVars(cID *big.Int, bFee uint64, pFee uint64) {
	chainID = cID
	signer = types.LatestSignerForChainID(chainID)
	priorityFee = new(big.Int).SetUint64(pFee * params.GWei)
	feeCap = new(big.Int).Add(new(big.Int).SetUint64(bFee*params.GWei), priorityFee)

	maxTransferCost = new(big.Int).Mul(new(big.Int).SetUint64(transferGasLimit), feeCap)
	maxTransferCost = new(big.Int).Add(maxTransferCost, transferAmount)

	requestAmount = new(big.Int).Mul(maxTransferCost, big.NewInt(100))
	minFunderBalance = new(big.Int).Add(maxTransferCost, requestAmount)
}

func createWorkers(ctx context.Context, keysDir string, endpoints []string, desiredWorkers int) (*worker, []*worker, error) {
	keys, err := key.LoadAll(ctx, keysDir)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to load keys: %w", err)
	}

	var master *worker
	var workers []*worker
	for i, k := range keys {
		worker, err := newWorker(k, endpoints[i%len(endpoints)], keysDir)
		if err != nil {
			return nil, nil, err
		}
		if err := worker.fetchBalance(ctx); err != nil {
			return nil, nil, err
		}
		if err := worker.fetchNonce(ctx); err != nil {
			return nil, nil, fmt.Errorf("could not get nonce: %w", err)
		}
		log.Printf("loaded worker %s (balance=%s nonce=%d)\n", worker.k.Address.Hex(), worker.balance.String(), worker.nonce)

		switch {
		case master == nil:
			master = worker
		case master.balance.Cmp(worker.balance) < 0:
			// We swap the worker with the master if it has a larger balance to
			// ensure the master is the key with the largest balance
			workers = append(workers, master)
			master = worker
		default:
			workers = append(workers, worker)
		}
	}

	if master == nil {
		master, err = newWorker(nil, endpoints[rand.Intn(len(endpoints))], keysDir)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to create master: %w", err)
		}
	}

	for len(workers) < desiredWorkers {
		i := len(workers)
		worker, err := newWorker(nil, endpoints[i%len(endpoints)], keysDir)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to create worker: %w", err)
		}

		workers = append(workers, worker)
	}

	return master, workers[:desiredWorkers], nil
}

type worker struct {
	c ethclient.Client
	k *key.Key

	balance *big.Int
	nonce   uint64
}

func newWorker(k *key.Key, endpoint string, keysDir string) (*worker, error) {
	client, err := ethclient.Dial(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create ethclient: %w", err)
	}

	if k == nil {
		k, err = key.Generate()
		if err != nil {
			return nil, fmt.Errorf("problem creating new private key: %w", err)
		}

		if err := k.Save(keysDir); err != nil {
			return nil, fmt.Errorf("could not save key: %w", err)
		}
	}

	return &worker{
		c:       client,
		k:       k,
		balance: big.NewInt(0),
		nonce:   0,
	}, nil
}

func (w *worker) fetchBalance(ctx context.Context) error {
	for ctx.Err() == nil {
		balance, err := w.c.BalanceAt(ctx, w.k.Address, nil)
		if err != nil {
			log.Printf("could not get balance: %s\n", err.Error())
			time.Sleep(retryDelay)
			continue
		}
		w.balance = balance
		return nil
	}
	return ctx.Err()
}

func (w *worker) fetchNonce(ctx context.Context) error {
	for ctx.Err() == nil {
		nonce, err := w.c.NonceAt(ctx, w.k.Address, nil)
		if err != nil {
			log.Printf("could not get nonce: %s\n", err.Error())
			time.Sleep(retryDelay)
			continue
		}
		w.nonce = nonce
		return nil
	}
	return ctx.Err()
}

func (w *worker) waitForBalance(ctx context.Context, stdout bool, minBalance *big.Int) error {
	for ctx.Err() == nil {
		if err := w.fetchBalance(ctx); err != nil {
			return fmt.Errorf("could not get balance: %w", err)
		}

		if w.balance.Cmp(minBalance) >= 0 {
			if stdout {
				log.Printf("found balance of %s\n", w.balance.String())
			}
			return nil
		}
		if stdout {
			log.Printf("waiting for balance of %s on %s\n", minBalance.String(), w.k.Address.Hex())
		}
		time.Sleep(5 * time.Second)
	}
	return ctx.Err()
}

func (w *worker) sendTx(ctx context.Context, recipient common.Address, value *big.Int) error {
	for ctx.Err() == nil {
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     w.nonce,
			To:        &recipient,
			Gas:       transferGasLimit,
			GasFeeCap: feeCap,
			GasTipCap: priorityFee,
			Value:     value,
			Data:      []byte{},
		})
		signedTx, err := types.SignTx(tx, signer, w.k.PrivKey)
		if err != nil {
			log.Printf("failed to sign transaction: %s", err.Error())
			time.Sleep(retryDelay)
			continue
		}
		if err := w.c.SendTransaction(ctx, signedTx); err != nil {
			log.Printf("failed to send transaction: %s", err.Error())
			time.Sleep(retryDelay)
			continue
		}
		txHash := signedTx.Hash()
		cost, err := w.confirmTransaction(ctx, txHash)
		if err != nil {
			log.Printf("failed to confirm %s: %s", txHash.Hex(), err.Error())
			time.Sleep(retryDelay)
			continue
		}
		w.nonce++
		w.balance = new(big.Int).Sub(w.balance, cost)
		w.balance = new(big.Int).Sub(w.balance, transferAmount)
		return nil
	}
	return ctx.Err()
}

func (w *worker) work(ctx context.Context, availableWorkers []*worker, fundRequest chan common.Address) error {
	for ctx.Err() == nil {
		if w.balance.Cmp(maxTransferCost) < 0 {
			log.Printf("%s requesting funds from master\n", w.k.Address.Hex())
			fundRequest <- w.k.Address
			if err := w.waitForBalance(ctx, false, maxTransferCost); err != nil {
				return fmt.Errorf("could not get balance: %w", err)
			}
		}
		recipient := availableWorkers[rand.Intn(len(availableWorkers))]
		if recipient.k.Address == w.k.Address {
			continue
		}
		if err := w.sendTx(ctx, recipient.k.Address, transferAmount); err != nil {
			return err
		}
		time.Sleep(workDelay)
	}
	return ctx.Err()
}

func (w *worker) fund(ctx context.Context, fundRequest chan common.Address) error {
	for {
		select {
		case recipient := <-fundRequest:
			if w.balance.Cmp(minFunderBalance) < 0 {
				if err := w.waitForBalance(ctx, true, minFunderBalance); err != nil {
					return fmt.Errorf("could not get minimum balance: %w", err)
				}
			}
			if err := w.sendTx(ctx, recipient, requestAmount); err != nil {
				return fmt.Errorf("unable to send tx: %w", err)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (w *worker) confirmTransaction(ctx context.Context, tx common.Hash) (*big.Int, error) {
	for ctx.Err() == nil {
		result, pending, _ := w.c.TransactionByHash(ctx, tx)
		if result == nil || pending {
			time.Sleep(retryDelay)
			continue
		}
		return result.Cost(), nil
	}
	return nil, ctx.Err()
}

// Run attempts to apply load to a network specified in .simulator/config.yml
// and periodically prints metrics about the traffic it generates.
func Run(ctx context.Context, cfg *Config, keysDir string) error {
	rclient, err := ethclient.Dial(cfg.Endpoints[0])
	if err != nil {
		return err
	}
	chainId, err := rclient.ChainID(ctx)
	if err != nil {
		return err
	}
	setupVars(chainId, cfg.BaseFee, cfg.PriorityFee)

	master, workers, err := createWorkers(ctx, keysDir, cfg.Endpoints, cfg.Concurrency)
	if err != nil {
		return fmt.Errorf("unable to load available workers: %w", err)
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return metrics.Monitor(gctx, rclient)
	})
	fundRequest := make(chan common.Address)
	g.Go(func() error {
		return master.fund(gctx, fundRequest)
	})
	for _, worker := range workers {
		w := worker
		g.Go(func() error {
			return w.work(gctx, workers, fundRequest)
		})
	}
	return g.Wait()
}
