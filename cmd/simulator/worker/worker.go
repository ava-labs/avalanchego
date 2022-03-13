package worker

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"math/rand"
	"time"

	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"golang.org/x/sync/errgroup"
)

const (
	cChainRPC = "ext/bc/C/rpc"
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

func LoadAvailableCWorkers(ctx context.Context, keys []*crypto.PrivateKeySECP256K1R, endpoints []string, desiredWorkers int) (*CWorker, []*CWorker, error) {
	var master *CWorker
	var workers []*CWorker
	for _, rpk := range keys {
		worker, err := New(rpk, endpoints[rand.Intn(len(endpoints))])
		if err != nil {
			return nil, nil, err
		}

		if err := worker.FetchBalance(ctx); err != nil {
			return nil, nil, err
		}

		log.Printf("loaded worker %s with balance %s\n", worker.addr.Hex(), worker.balance.String())
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

type CWorker struct {
	c    *ethclient.Client
	pk   *ecdsa.PrivateKey
	addr common.Address

	balance *big.Int
}

func New(rpk *crypto.PrivateKeySECP256K1R, endpoint string) (*CWorker, error) {
	client, err := ethclient.Dial(fmt.Sprintf("%s/%s", endpoint, cChainRPC))
	if err != nil {
		return nil, fmt.Errorf("failed to create ethclient: %w", err)
	}

	if rpk == nil {
		rpk, err = GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("problem creating new private key: %w", err)
		}

		if err := SaveKey(rpk); err != nil {
			return nil, fmt.Errorf("could not save key: %w", err)
		}
	}

	pk := rpk.ToECDSA()
	addr := ethcrypto.PubkeyToAddress(pk.PublicKey)
	return &CWorker{
		c:       client,
		pk:      pk,
		addr:    addr,
		balance: big.NewInt(0),
	}, nil
}

func (w *CWorker) FetchBalance(ctx context.Context) error {
	balance, err := w.c.BalanceAt(ctx, w.addr, nil)
	if err != nil {
		return fmt.Errorf("could not get balance: %w", err)
	}

	w.balance = balance
	return nil
}

func (w *CWorker) waitForBalance(ctx context.Context, stdout bool, minBalance *big.Int) error {
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
			log.Printf("waiting for balance of %s on %s\n", minBalance.String(), w.addr.Hex())
		}
		time.Sleep(5 * time.Second)
	}
}

// TODO: support X import/export once mempool and txStatus endpoint is merged
// func (w *CWorker) importX() error {
// }
//
// func (w *CWorker) exportX(recipient ids.ShortID, amount uint64) error {
// }

func (w *CWorker) Work(ctx context.Context, availableCWorkers []*CWorker, fundRequest chan common.Address) error {
	nonce, err := w.c.PendingNonceAt(ctx, w.addr)
	if err != nil {
		return fmt.Errorf("could not get nonce: %w", err)
	}

	for ctx.Err() == nil {
		if new(big.Int).Sub(w.balance, transferBalance).Sign() < 0 {
			log.Printf("%s requesting funds from master\n", w.addr.Hex())
			fundRequest <- w.addr
			if err := w.waitForBalance(ctx, false, transferBalance); err != nil {
				return fmt.Errorf("could not get minimum balance: %w", err)
			}
		}

		recipient := availableCWorkers[rand.Intn(len(availableCWorkers))]
		if recipient.addr == w.addr {
			continue
		}

		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			To:        &recipient.addr,
			Gas:       transferGasLimit,
			GasFeeCap: transferGasPrice,
			GasTipCap: common.Big0,
			Data:      []byte{},
		})
		signedTx, err := types.SignTx(tx, signer, w.pk)
		if err != nil {
			return fmt.Errorf("failed to sign transaction: %w", err)
		}

		if err := w.c.SendTransaction(ctx, signedTx); err != nil {
			// return fmt.Errorf("unable to send tx: %w", err)
			time.Sleep(workDelay)
			continue
		}

		log.Printf("%s broadcasted transaction: %s\n", w.addr.Hex(), signedTx.Hash().Hex())

		if err := w.confirmTransaction(ctx, nonce); err != nil {
			return fmt.Errorf("unable to confirm %s: %w", signedTx.Hash().Hex(), err)
		}

		nonce++
		w.balance = new(big.Int).Sub(w.balance, transferBalance)

		time.Sleep(workDelay)
	}

	return ctx.Err()
}

func (w *CWorker) Fund(ctx context.Context, fundRequest chan common.Address) error {
	nonce, err := w.c.PendingNonceAt(ctx, w.addr)
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

			tx := types.NewTransaction(
				nonce,
				recipient,
				requestBalance,
				transferGasLimit,
				transferGasPrice,
				nil,
			)

			signedTx, err := types.SignTx(tx, signer, w.pk)
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

func (w *CWorker) confirmTransaction(ctx context.Context, broadcastNonce uint64) error {
	for ctx.Err() == nil {
		nonce, err := w.c.NonceAt(ctx, w.addr, nil)
		if err == nil && nonce >= broadcastNonce {
			return nil
		}

		time.Sleep(2 * time.Second)
		continue
	}

	return ctx.Err()
}

func RunLoad(ctx context.Context, endpoints []string, cWorkers int, maxBaseFee uint64, maxPriorityFee uint64) error {
	rpks, err := LoadAvailableKeys(ctx)
	if err != nil {
		return fmt.Errorf("unable to load keys: %w", err)
	}

	master, workers, err := LoadAvailableCWorkers(ctx, rpks, endpoints, cWorkers)
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
