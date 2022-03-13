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

func SetupVars(cID *big.Int, bFee uint64, pFee uint64) {
	chainID = cID
	signer = types.LatestSignerForChainID(chainID)
	priorityFee = new(big.Int).SetUint64(pFee * params.GWei)
	feeCap = new(big.Int).Add(new(big.Int).SetUint64(bFee*params.GWei), priorityFee)

	maxTransferCost = new(big.Int).Mul(new(big.Int).SetUint64(transferGasLimit), feeCap)
	maxTransferCost = new(big.Int).Add(maxTransferCost, transferAmount)

	requestAmount = new(big.Int).Mul(maxTransferCost, big.NewInt(100))
	minFunderBalance = new(big.Int).Add(maxTransferCost, requestAmount)
}

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
		if err := worker.FetchNonce(ctx); err != nil {
			return nil, nil, fmt.Errorf("could not get nonce: %w", err)
		}
		log.Printf("loaded worker %s (balance=%s nonce=%d)\n", worker.k.addr.Hex(), worker.balance.String(), worker.nonce)

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
	nonce   uint64
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
		nonce:   0,
	}, nil
}

func (w *Worker) FetchBalance(ctx context.Context) error {
	for ctx.Err() == nil {
		balance, err := w.c.BalanceAt(ctx, w.k.addr, nil)
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

func (w *Worker) FetchNonce(ctx context.Context) error {
	for ctx.Err() == nil {
		nonce, err := w.c.PendingNonceAt(ctx, w.k.addr)
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

func (w *Worker) waitForBalance(ctx context.Context, stdout bool, minBalance *big.Int) error {
	for ctx.Err() == nil {
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
	return ctx.Err()
}

func (w *Worker) sendTx(ctx context.Context, recipient common.Address, value *big.Int) error {
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
		signedTx, err := types.SignTx(tx, signer, w.k.pk)
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
		log.Printf("%s broadcasted transaction: %s\n", w.k.addr.Hex(), txHash.Hex())
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

func (w *Worker) Work(ctx context.Context, availableWorkers []*Worker, fundRequest chan common.Address) error {
	for ctx.Err() == nil {
		if new(big.Int).Sub(w.balance, maxTransferCost).Sign() < 0 {
			log.Printf("%s requesting funds from master\n", w.k.addr.Hex())
			fundRequest <- w.k.addr
			if err := w.waitForBalance(ctx, false, maxTransferCost); err != nil {
				return fmt.Errorf("could not get balance: %w", err)
			}
		}
		recipient := availableWorkers[rand.Intn(len(availableWorkers))]
		if recipient.k.addr == w.k.addr {
			continue
		}
		if err := w.sendTx(ctx, recipient.k.addr, transferAmount); err != nil {
			return err
		}
		time.Sleep(workDelay)
	}
	return ctx.Err()
}

func (w *Worker) Fund(ctx context.Context, fundRequest chan common.Address) error {
	for {
		select {
		case recipient := <-fundRequest:
			if new(big.Int).Sub(w.balance, minFunderBalance).Sign() < 0 {
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

func (w *Worker) confirmTransaction(ctx context.Context, tx common.Hash) (*big.Int, error) {
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

func Run(ctx context.Context, endpoints []string, concurrency int, baseFee uint64, priorityFee uint64) error {
	rclient, err := ethclient.Dial(endpoints[0])
	if err != nil {
		return err
	}
	chainId, err := rclient.ChainID(ctx)
	if err != nil {
		return err
	}
	SetupVars(chainId, baseFee, priorityFee)

	ks, err := LoadAvailableKeys(ctx)
	if err != nil {
		return fmt.Errorf("unable to load keys: %w", err)
	}
	master, workers, err := CreateWorkers(ctx, ks, endpoints, concurrency)
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
