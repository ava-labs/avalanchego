// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/accounts/abi/bind"
	"github.com/ava-labs/avalanchego/graft/coreth/ethclient"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/utils/logging"

	timerpkg "github.com/ava-labs/avalanchego/utils/timer"
	ethereum "github.com/ava-labs/libevm"
	ethcommon "github.com/ava-labs/libevm/common"
	ethparams "github.com/ava-labs/libevm/params"
)

const (
	cChainTxGasLimit    = 50_000 // gas limit for transactions with data payloads
	cChainMaxTxDataSize = 64     // max random data bytes per transaction
)

// cChainWorkload issues EVM transactions on the C-Chain.
type cChainWorkload struct {
	id      int
	log     logging.Logger
	chainID *big.Int
	key     *ecdsa.PrivateKey
	uris    []string
	// nodeClient is the ethclient this worker uses to submit transactions.
	nodeClient *ethclient.Client
	// verifyClients has one ethclient per URI for cross-node verification.
	verifyClients []*ethclient.Client
}

// newTestContext returns a test context that ensures that log output and
// assertions are associated with this C-Chain worker.
func (w *cChainWorkload) newTestContext(ctx context.Context) *tests.SimpleTestContext {
	return antithesis.NewInstrumentedTestContextWithArgs(
		ctx,
		w.log,
		map[string]any{
			"cChainWorker": w.id,
		},
	)
}

func (w *cChainWorkload) run(ctx context.Context) {
	timer := timerpkg.StoppedTimer()

	tc := w.newTestContext(ctx)
	defer tc.RecoverAndRethrow()
	require := require.New(tc)

	balance, err := w.nodeClient.BalanceAt(ctx, crypto.PubkeyToAddress(w.key.PublicKey), nil)
	require.NoError(err, "failed to fetch C-Chain balance")
	assert.Reachable("C-Chain worker starting", map[string]any{
		"worker":  w.id,
		"balance": balance,
	})

	for {
		w.executeTest(ctx)

		val, err := rand.Int(rand.Reader, big.NewInt(int64(time.Second)))
		require.NoError(err, "failed to read randomness")

		timer.Reset(time.Duration(val.Int64()))
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

func (w *cChainWorkload) executeTest(ctx context.Context) {
	tc := w.newTestContext(ctx)
	// Panics will be recovered without being rethrown, ensuring that
	// test failures are not fatal and the worker continues.
	defer tc.Recover()

	w.log.Info("executing issueCChainTransfer")
	w.issueCChainTransfer(ctx)

	// Add more test tx types here...
}

// issueCChainTransfer sends a self-transfer, waits for acceptance, then
// verifies the transaction data on all nodes via confirmCChainTx.
func (w *cChainWorkload) issueCChainTransfer(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	// ~50% of transactions include random data to vary block body content.
	var txData []byte
	includeData, err := rand.Int(rand.Reader, big.NewInt(2))
	if err != nil {
		w.log.Warn("failed to generate random flag", zap.Error(err))
		return
	}
	if includeData.Int64() == 1 {
		dataLen, err := rand.Int(rand.Reader, big.NewInt(cChainMaxTxDataSize))
		if err != nil {
			w.log.Warn("failed to generate random data length", zap.Error(err))
			return
		}
		txData = make([]byte, dataLen.Int64()+1) // 1 to cChainMaxTxDataSize bytes
		if _, err := rand.Read(txData); err != nil {
			w.log.Warn("failed to generate random data", zap.Error(err))
			return
		}
	}

	gasLimit := ethparams.TxGas
	if len(txData) > 0 {
		gasLimit = cChainTxGasLimit
	}

	// Random amount (1000-100999 wei) to vary tx hashes and block content.
	// Value round-trips since we send to self; only gas is consumed.
	randVal, err := rand.Int(rand.Reader, big.NewInt(100_000))
	if err != nil {
		w.log.Warn("failed to generate random value", zap.Error(err))
		return
	}
	txAmount := new(big.Int).Add(randVal, big.NewInt(1000))

	recipientAddr := crypto.PubkeyToAddress(w.key.PublicKey)
	tx, err := w.sendCChainTx(ctx, w.nodeClient, recipientAddr, txAmount, txData, gasLimit)
	if err != nil {
		w.log.Warn("failed to send C-Chain transfer",
			zap.Error(err),
		)
		return
	}

	startTime := time.Now()
	w.log.Info("issued C-Chain transfer",
		zap.Stringer("txID", tx.Hash()),
		zap.Uint64("nonce", tx.Nonce()),
		zap.Stringer("value", txAmount),
		zap.Int("dataLen", len(txData)),
	)

	receipt, err := bind.WaitMined(ctx, w.nodeClient, tx)
	if err != nil {
		w.log.Warn("failed to wait for C-Chain transaction receipt",
			zap.Stringer("txID", tx.Hash()),
			zap.Error(err),
		)
		return
	}

	w.log.Info("accepted C-Chain transaction",
		zap.Stringer("txID", tx.Hash()),
		zap.Uint64("gasUsed", receipt.GasUsed),
		zap.Stringer("blockNumber", receipt.BlockNumber),
		zap.Duration("duration", time.Since(startTime)),
	)

	if err := w.confirmCChainTx(ctx, tx); err != nil {
		w.log.Warn("failed to confirm C-Chain transaction",
			zap.Stringer("txID", tx.Hash()),
			zap.Error(err),
		)
	}
}

// sendCChainTx builds, signs, and sends a C-Chain EIP-1559 transaction.
// It is used both for workload transfers and initial worker funding.
func (w *cChainWorkload) sendCChainTx(
	ctx context.Context,
	client *ethclient.Client,
	to ethcommon.Address,
	value *big.Int,
	data []byte,
	gasLimit uint64,
) (*types.Transaction, error) {
	senderAddr := crypto.PubkeyToAddress(w.key.PublicKey)

	acceptedNonce, err := client.AcceptedNonceAt(ctx, senderAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch accepted nonce: %w", err)
	}
	gasTipCap, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch suggested gas tip: %w", err)
	}
	gasFeeCap, err := client.EstimateBaseFee(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch estimated base fee: %w", err)
	}

	signer := types.LatestSignerForChainID(w.chainID)
	tx, err := types.SignNewTx(w.key, signer, &types.DynamicFeeTx{
		ChainID:   w.chainID,
		Nonce:     acceptedNonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       gasLimit,
		To:        &to,
		Value:     value,
		Data:      data,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	if err := client.SendTransaction(ctx, tx); err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}
	return tx, nil
}

// awaitCChainTxReceipt polls the node until the transaction receipt is
// available or the context is cancelled.
func awaitCChainTxReceipt(ctx context.Context, client *ethclient.Client, txHash ethcommon.Hash) (*types.Receipt, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil {
			return receipt, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// confirmCChainTx waits for each node to have the transaction, then verifies
// data integrity across all nodes: receipt, tx fields, block structure,
// account nonce advancement, and balance consistency.
func (w *cChainWorkload) confirmCChainTx(ctx context.Context, sentTx *types.Transaction) error {
	txHash := sentTx.Hash()
	senderAddr := crypto.PubkeyToAddress(w.key.PublicKey)

	for i, client := range w.verifyClients {
		uri := w.uris[i]

		receipt, err := awaitCChainTxReceipt(ctx, client, txHash)
		if err != nil {
			return fmt.Errorf("timed out waiting for tx %s on %s: %w", txHash, uri, err)
		}

		if receipt.TxHash != txHash {
			assert.Unreachable("C-Chain receipt tx hash mismatch", map[string]any{
				"worker":   w.id,
				"uri":      uri,
				"expected": txHash,
				"actual":   receipt.TxHash,
			})
			return fmt.Errorf("receipt tx hash mismatch on %s: got %s, want %s", uri, receipt.TxHash, txHash)
		}

		if receipt.Status != types.ReceiptStatusSuccessful {
			assert.Unreachable("C-Chain transaction receipt indicates failure", map[string]any{
				"worker": w.id,
				"uri":    uri,
				"txID":   txHash,
				"status": receipt.Status,
			})
			return fmt.Errorf("tx %s failed on %s with status %d", txHash, uri, receipt.Status)
		}

		// Verify transaction fields.
		fetchedTx, _, err := client.TransactionByHash(ctx, txHash)
		if err != nil {
			if errors.Is(err, ethereum.NotFound) {
				assert.Unreachable("C-Chain transaction not found after receipt confirmed", map[string]any{
					"worker": w.id,
					"uri":    uri,
					"txID":   txHash,
				})
			}
			return fmt.Errorf("failed to fetch tx %s on %s: %w", txHash, uri, err)
		}

		if fetchedTx.Hash() != sentTx.Hash() {
			assert.Unreachable("C-Chain transaction hash mismatch", map[string]any{
				"worker":   w.id,
				"uri":      uri,
				"expected": sentTx.Hash(),
				"actual":   fetchedTx.Hash(),
			})
			return fmt.Errorf("tx hash mismatch on %s: got %s, want %s", uri, fetchedTx.Hash(), sentTx.Hash())
		}

		// Verify block structure consistency.
		blockByNum, err := client.BlockByNumber(ctx, receipt.BlockNumber)
		if err != nil {
			if errors.Is(err, ethereum.NotFound) {
				assert.Unreachable("block not found by number after receipt confirmed", map[string]any{
					"worker":      w.id,
					"uri":         uri,
					"txID":        txHash,
					"blockNumber": receipt.BlockNumber,
				})
			}
			return fmt.Errorf("failed to fetch block %s by number on %s: %w", receipt.BlockNumber, uri, err)
		}

		blockByHash, err := client.BlockByHash(ctx, receipt.BlockHash)
		if err != nil {
			if errors.Is(err, ethereum.NotFound) {
				assert.Unreachable("block not found by hash after receipt confirmed", map[string]any{
					"worker":    w.id,
					"uri":       uri,
					"txID":      txHash,
					"blockHash": receipt.BlockHash,
				})
			}
			return fmt.Errorf("failed to fetch block %s by hash on %s: %w", receipt.BlockHash, uri, err)
		}

		if blockByNum.Hash() != blockByHash.Hash() {
			assert.Unreachable("block hash mismatch between BlockByNumber and BlockByHash", map[string]any{
				"worker":   w.id,
				"uri":      uri,
				"txID":     txHash,
				"byNumber": blockByNum.Hash(),
				"byHash":   blockByHash.Hash(),
			})
			return fmt.Errorf("block hash mismatch on %s: byNumber=%s byHash=%s", uri, blockByNum.Hash(), blockByHash.Hash())
		}

		if blockByNum.Hash() != receipt.BlockHash {
			assert.Unreachable("block hash does not match receipt", map[string]any{
				"worker":           w.id,
				"uri":              uri,
				"txID":             txHash,
				"blockHash":        blockByNum.Hash(),
				"receiptBlockHash": receipt.BlockHash,
			})
			return fmt.Errorf("block hash %s != receipt block hash %s on %s", blockByNum.Hash(), receipt.BlockHash, uri)
		}

		// Verify transaction is in the block.
		blockTxs := blockByNum.Transactions()
		found := false
		for _, blockTx := range blockTxs {
			if blockTx.Hash() == txHash {
				found = true
				break
			}
		}
		if !found {
			assert.Unreachable("transaction not found in block", map[string]any{
				"worker":       w.id,
				"uri":          uri,
				"txID":         txHash,
				"blockNumber":  blockByNum.Number(),
				"blockTxCount": len(blockTxs),
			})
			return fmt.Errorf("tx %s not found in block %s (%d txs) on %s", txHash, blockByNum.Number(), len(blockTxs), uri)
		}

		// Verify account nonce advanced.
		postNonce, err := client.NonceAt(ctx, senderAddr, receipt.BlockNumber)
		if err == nil {
			expectedNonce := sentTx.Nonce() + 1
			if postNonce < expectedNonce {
				assert.Unreachable("account nonce did not advance after tx", map[string]any{
					"worker":   w.id,
					"uri":      uri,
					"txID":     txHash,
					"expected": expectedNonce,
					"actual":   postNonce,
				})
				return fmt.Errorf("nonce on %s: got %d, want >= %d", uri, postNonce, expectedNonce)
			}
		}

		// Verify balance consistency for self-transfers.
		preBlockNum := new(big.Int).Sub(receipt.BlockNumber, big.NewInt(1))
		preBalance, preErr := client.BalanceAt(ctx, senderAddr, preBlockNum)
		postBalance, postErr := client.BalanceAt(ctx, senderAddr, receipt.BlockNumber)
		if preErr == nil && postErr == nil && receipt.EffectiveGasPrice != nil {
			gasCost := new(big.Int).Mul(
				new(big.Int).SetUint64(receipt.GasUsed),
				receipt.EffectiveGasPrice,
			)
			expectedPostBalance := new(big.Int).Sub(preBalance, gasCost)
			if postBalance.Cmp(expectedPostBalance) != 0 {
				assert.Unreachable("C-Chain balance mismatch after self-transfer", map[string]any{
					"worker":       w.id,
					"uri":          uri,
					"txID":         txHash,
					"preBalance":   preBalance,
					"postBalance":  postBalance,
					"expectedPost": expectedPostBalance,
					"gasCost":      gasCost,
				})
				return fmt.Errorf("balance mismatch on %s: post=%s, expected=%s (pre=%s, gas=%s)",
					uri, postBalance, expectedPostBalance, preBalance, gasCost)
			}
		}

		w.log.Info("confirmed C-Chain transaction",
			zap.Stringer("txID", txHash),
			zap.String("uri", uri),
		)
	}

	w.log.Info("confirmed C-Chain transaction on all nodes",
		zap.Stringer("txID", txHash),
	)
	assert.Reachable("confirmed C-Chain transaction on all nodes", map[string]any{
		"worker": w.id,
		"txID":   txHash,
	})
	return nil
}

func (w *cChainWorkload) fundCChainWorker(ctx context.Context, recipientAddr ethcommon.Address, amount uint64, log logging.Logger) error {
	tx, err := w.sendCChainTx(ctx, w.nodeClient, recipientAddr, big.NewInt(int64(amount)), nil, ethparams.TxGas)
	if err != nil {
		return fmt.Errorf("failed to send funding tx: %w", err)
	}

	log.Info("sending C-Chain funding transaction",
		zap.Stringer("txID", tx.Hash()),
		zap.Uint64("nonce", tx.Nonce()),
	)

	if _, err := bind.WaitMined(ctx, w.nodeClient, tx); err != nil {
		return fmt.Errorf("failed to wait for funding receipt: %w", err)
	}
	log.Info("confirmed C-Chain funding transaction",
		zap.Stringer("txID", tx.Hash()),
	)
	return nil
}
