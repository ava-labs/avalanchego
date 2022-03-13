package worker

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"math/rand"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"golang.org/x/sync/errgroup"
)

var (
	transferGasLimit = uint64(21000)
	transferGasPrice = big.NewInt(225 * params.GWei)
	transferGas      = new(big.Int).Mul(big.NewInt(int64(transferGasLimit)), transferGasPrice)
	transferAmount   = big.NewInt(1)

	transferBalance  = new(big.Int).Add(transferGas, transferAmount)
	requestBalance   = new(big.Int).Mul(transferBalance, big.NewInt(50))
	minFunderBalance = new(big.Int).Add(transferBalance, requestBalance)

	chainID = big.NewInt(43112)
	signer  = types.LatestSignerForChainID(chainID)

	workDelay = time.Duration(100 * time.Millisecond)
)

func CreateWorkers(ctx context.Context, keys []*Key, endpoints []string, desiredWorkers int) (*Worker, []*Worker, error) {
	var master *Worker
	var workers []*Worker
	for _, k := range keys {
		worker, err := New(k, endpoints[rand.Intn(len(endpoints))])
		if err != nil {
			return nil, nil, err
		}

		if err := worker.FetchBalance(ctx); err != nil {
			return nil, nil, err
		}

		log.Printf("loaded worker %s with balance %s\n", worker.k.addr.Hex(), worker.balance.String())
		switch {
		case master == nil:
			master = worker
		case new(big.Int).Sub(master.balance, worker.balance).Sign() < 0:
			workers = append(workers, master)
			master = worker
		default:
			workers = append(workers, worker)
		}
	}

	var err error
	if master == nil {
		master, err = New(nil, endpoints[rand.Intn(len(endpoints))])
		if err != nil {
			return nil, nil, fmt.Errorf("unable to create master: %w", err)
		}
	}

	for len(workers) < desiredWorkers {
		worker, err := New(nil, endpoints[rand.Intn(len(endpoints))])
		if err != nil {
			return nil, nil, fmt.Errorf("unable to create worker: %w", err)
		}

		workers = append(workers, worker)
	}

	return master, workers[:desiredWorkers], nil
}

type Worker struct {
	c *ethclient.Client
	k *Key

	balance *big.Int
}

func New(k *Key, endpoint string) (*Worker, error) {
	client, err := ethclient.Dial(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create ethclient: %w", err)
	}

	if k == nil {
		k, err = GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("problem creating new private key: %w", err)
		}

		if err := SaveKey(k); err != nil {
			return nil, fmt.Errorf("could not save key: %w", err)
		}
	}

	return &Worker{
		c:       client,
		k:       k,
		balance: big.NewInt(0),
	}, nil
}

func (w *Worker) FetchBalance(ctx context.Context) error {
	balance, err := w.c.BalanceAt(ctx, w.k.addr, nil)
	if err != nil {
		return fmt.Errorf("could not get balance: %w", err)
	}

	w.balance = balance
	return nil
}

func (w *Worker) waitForBalance(ctx context.Context, stdout bool, minBalance *big.Int) error {
	for {
		if err := w.FetchBalance(ctx); err != nil {
			return fmt.Errorf("could not get balance: %w", err)
		}

		if new(big.Int).Sub(w.balance, minBalance).Sign() >= 0 {
			if stdout {
				log.Printf("found balance of %s\n", w.balance.String())
			}
			return nil
		}
		if stdout {
			log.Printf("waiting for balance of %s on %s\n", minBalance.String(), w.k.addr.Hex())
		}
		time.Sleep(5 * time.Second)
	}
}

func (w *Worker) Work(ctx context.Context, availableWorkers []*Worker, fundRequest chan common.Address) error {
	nonce, err := w.c.PendingNonceAt(ctx, w.k.addr)
	if err != nil {
		return fmt.Errorf("could not get nonce: %w", err)
	}

	for ctx.Err() == nil {
		if new(big.Int).Sub(w.balance, transferBalance).Sign() < 0 {
			log.Printf("%s requesting funds from master\n", w.k.addr.Hex())
			fundRequest <- w.k.addr
			if err := w.waitForBalance(ctx, false, transferBalance); err != nil {
				return fmt.Errorf("could not get minimum balance: %w", err)
			}
		}

		recipient := availableWorkers[rand.Intn(len(availableWorkers))]
		if recipient.k.addr == w.k.addr {
			continue
		}

		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID: chainID,
			Nonce:   nonce,
			To:      &recipient.k.addr,
			Gas:     transferGasLimit,
			// TODO: replace
			GasFeeCap: transferGasPrice,
			GasTipCap: common.Big0,
			Data:      []byte{},
		})
		signedTx, err := types.SignTx(tx, signer, w.k.pk)
		if err != nil {
			return fmt.Errorf("failed to sign transaction: %w", err)
		}

		if err := w.c.SendTransaction(ctx, signedTx); err != nil {
			// return fmt.Errorf("unable to send tx: %w", err)
			time.Sleep(workDelay)
			continue
		}

		log.Printf("%s broadcasted transaction: %s\n", w.k.addr.Hex(), signedTx.Hash().Hex())

		if err := w.confirmTransaction(ctx, nonce); err != nil {
			return fmt.Errorf("unable to confirm %s: %w", signedTx.Hash().Hex(), err)
		}

		nonce++
		// TODO: need to pull receipt to see how much went
		w.balance = new(big.Int).Sub(w.balance, transferBalance)

		time.Sleep(workDelay)
	}

	return ctx.Err()
}

func (w *Worker) Fund(ctx context.Context, fundRequest chan common.Address) error {
	nonce, err := w.c.PendingNonceAt(ctx, w.k.addr)
	if err != nil {
		return fmt.Errorf("could not get nonce: %w", err)
	}

	for {
		select {
		case recipient := <-fundRequest:
			if new(big.Int).Sub(w.balance, minFunderBalance).Sign() < 0 {
				if err := w.waitForBalance(ctx, true, minFunderBalance); err != nil {
					return fmt.Errorf("could not get minimum balance: %w", err)
				}
			}

			// TODO: unify to single tx func
			tx := types.NewTransaction(
				nonce,
				recipient,
				requestBalance,
				transferGasLimit,
				transferGasPrice,
				nil,
			)

			signedTx, err := types.SignTx(tx, signer, w.k.pk)
			if err != nil {
				return fmt.Errorf("failed to sign transaction: %w", err)
			}

			if err := w.c.SendTransaction(ctx, signedTx); err != nil {
				return fmt.Errorf("unable to send tx: %w", err)
			}
			log.Printf("broadcasted funding transaction: %s\n", signedTx.Hash().Hex())

			if err := w.confirmTransaction(ctx, nonce); err != nil {
				return fmt.Errorf("unable to confirm %s: %w", signedTx.Hash().Hex(), err)
			}

			nonce++
			w.balance = new(big.Int).Sub(w.balance, requestBalance)
			w.balance = new(big.Int).Sub(w.balance, transferGas)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (w *Worker) confirmTransaction(ctx context.Context, broadcastNonce uint64) error {
	for ctx.Err() == nil {
		// TODO: get receipt
		nonce, err := w.c.NonceAt(ctx, w.k.addr, nil)
		if err == nil && nonce >= broadcastNonce {
			return nil
		}

		time.Sleep(2 * time.Second)
		continue
	}

	return ctx.Err()
}

func Run(ctx context.Context, endpoints []string, cWorkers int, maxBaseFee uint64, maxPriorityFee uint64) error {
	rpks, err := LoadAvailableKeys(ctx)
	if err != nil {
		return fmt.Errorf("unable to load keys: %w", err)
	}

	master, workers, err := CreateWorkers(ctx, rpks, endpoints, cWorkers)
	if err != nil {
		return fmt.Errorf("unable to load available workers: %w", err)
	}

	g, gctx := errgroup.WithContext(ctx)
	fundRequest := make(chan common.Address)
	g.Go(func() error {
		return master.Fund(gctx, fundRequest)
	})
	for _, worker := range workers {
		w := worker
		g.Go(func() error {
			return w.Work(gctx, workers, fundRequest)
		})
	}
	return g.Wait()
}
