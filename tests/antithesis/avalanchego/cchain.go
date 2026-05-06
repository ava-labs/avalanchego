// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
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

	ethereum "github.com/ava-labs/libevm"

	"github.com/ava-labs/avalanchego/graft/coreth/accounts/abi/bind"
	"github.com/ava-labs/avalanchego/graft/coreth/ethclient"
	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/utils/logging"

	timerpkg "github.com/ava-labs/avalanchego/utils/timer"
	ethcommon "github.com/ava-labs/libevm/common"
	ethparams "github.com/ava-labs/libevm/params"
)

const (
	cchainTxGasLimit    = 50_000 // gas limit for transactions with data payloads
	cchainMaxTxDataSize = 64     // max random data bytes per transaction
)

// cchainWorkload issues EVM transactions on the C-chain, analogous to the
// X/P-chain workload in main.go.
type cchainWorkload struct {
	id     int
	log    logging.Logger
	client *ethclient.Client
	key    *ecdsa.PrivateKey
	uris   []string
}

func (w *cchainWorkload) run(ctx context.Context) {
	timer := timerpkg.StoppedTimer()

	tc := antithesis.NewInstrumentedTestContextWithArgs(
		ctx,
		w.log,
		map[string]any{
			"cchainWorker": w.id,
		},
	)
	defer tc.RecoverAndRethrow()
	require := require.New(tc)

	balance, err := w.client.BalanceAt(ctx, crypto.PubkeyToAddress(w.key.PublicKey), nil)
	require.NoError(err, "failed to fetch C-chain balance")
	assert.Reachable("C-chain worker starting", map[string]any{
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

func (w *cchainWorkload) executeTest(ctx context.Context) {
	w.log.Info("executing issueCChainTransfer")
	w.issueCChainTransfer(ctx)
}

// issueCChainTransfer sends a self-transfer, waits for acceptance, then
// verifies the transaction data on all nodes via confirmCChainTx.
func (w *cchainWorkload) issueCChainTransfer(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	randVal, err := rand.Int(rand.Reader, big.NewInt(100_000))
	if err != nil {
		w.log.Warn("failed to generate random value", zap.Error(err))
		return
	}
	txAmount := new(big.Int).Add(randVal, big.NewInt(1000))

	// ~50% of transactions include random data to vary block body content.
	var txData []byte
	includeData, err := rand.Int(rand.Reader, big.NewInt(2))
	if err != nil {
		w.log.Warn("failed to generate random flag", zap.Error(err))
		return
	}
	if includeData.Int64() == 1 {
		dataLen, err := rand.Int(rand.Reader, big.NewInt(cchainMaxTxDataSize))
		if err != nil {
			w.log.Warn("failed to generate random data length", zap.Error(err))
			return
		}
		txData = make([]byte, dataLen.Int64()+1) // 1 to cchainMaxTxDataSize bytes
		if _, err := rand.Read(txData); err != nil {
			w.log.Warn("failed to generate random data", zap.Error(err))
			return
		}
	}

	// Send to self so the worker balance stays funded for continuous testing.
	recipientAddress := crypto.PubkeyToAddress(w.key.PublicKey)

	chainID, err := w.client.ChainID(ctx)
	if err != nil {
		w.log.Warn("failed to fetch chainID", zap.Error(err))
		return
	}
	acceptedNonce, err := w.client.AcceptedNonceAt(ctx, recipientAddress)
	if err != nil {
		w.log.Warn("failed to fetch accepted nonce", zap.Error(err))
		return
	}
	gasTipCap, err := w.client.SuggestGasTipCap(ctx)
	if err != nil {
		w.log.Warn("failed to fetch suggested gas tip", zap.Error(err))
		return
	}
	gasFeeCap, err := w.client.EstimateBaseFee(ctx)
	if err != nil {
		w.log.Warn("failed to fetch estimated base fee", zap.Error(err))
		return
	}

	gasLimit := ethparams.TxGas
	if len(txData) > 0 {
		gasLimit = cchainTxGasLimit
	}

	signer := types.LatestSignerForChainID(chainID)
	tx, err := types.SignNewTx(w.key, signer, &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     acceptedNonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       gasLimit,
		To:        &recipientAddress,
		Value:     txAmount,
		Data:      txData,
	})
	if err != nil {
		w.log.Warn("failed to sign C-chain transaction", zap.Error(err))
		return
	}

	startTime := time.Now()
	if err := w.client.SendTransaction(ctx, tx); err != nil {
		w.log.Warn("failed to send C-chain transaction",
			zap.Stringer("txID", tx.Hash()),
			zap.Error(err),
		)
		return
	}

	w.log.Info("issued C-chain transfer",
		zap.Stringer("txID", tx.Hash()),
		zap.Uint64("nonce", acceptedNonce),
		zap.Stringer("value", txAmount),
		zap.Int("dataLen", len(txData)),
	)

	receipt, err := bind.WaitMined(ctx, w.client, tx)
	if err != nil {
		w.log.Warn("failed to wait for C-chain transaction receipt",
			zap.Stringer("txID", tx.Hash()),
			zap.Error(err),
		)
		return
	}

	w.log.Info("accepted C-chain transaction",
		zap.Stringer("txID", tx.Hash()),
		zap.Uint64("gasUsed", receipt.GasUsed),
		zap.Stringer("blockNumber", receipt.BlockNumber),
		zap.Duration("duration", time.Since(startTime)),
	)

	w.confirmCChainTx(ctx, tx)
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
// data integrity across all nodes.
func (w *cchainWorkload) confirmCChainTx(ctx context.Context, sentTx *types.Transaction) {
	txHash := sentTx.Hash()

	for _, uri := range w.uris {
		cchainURI := fmt.Sprintf("%s/ext/bc/C/rpc", uri)
		client, err := ethclient.Dial(cchainURI)
		if err != nil {
			w.log.Warn("failed to dial C-chain RPC for confirmation",
				zap.String("uri", uri),
				zap.Error(err),
			)
			return
		}

		// Wait for the transaction receipt on this node.
		receipt, err := awaitCChainTxReceipt(ctx, client, txHash)
		if err != nil {
			w.log.Warn("timed out waiting for C-chain transaction on node",
				zap.String("uri", uri),
				zap.Stringer("txID", txHash),
				zap.Error(err),
			)
			return
		}

		// Verify receipt fields.
		if receipt.TxHash != txHash {
			w.log.Error("C-chain receipt tx hash mismatch",
				zap.String("uri", uri),
				zap.Stringer("expected", txHash),
				zap.Stringer("actual", receipt.TxHash),
			)
			assert.Unreachable("C-chain receipt tx hash mismatch", map[string]any{
				"worker":   w.id,
				"uri":      uri,
				"expected": txHash,
				"actual":   receipt.TxHash,
			})
			return
		}

		if receipt.Status != types.ReceiptStatusSuccessful {
			w.log.Error("C-chain transaction receipt indicates failure",
				zap.String("uri", uri),
				zap.Stringer("txID", txHash),
				zap.Uint64("status", receipt.Status),
			)
			assert.Unreachable("C-chain transaction receipt indicates failure", map[string]any{
				"worker": w.id,
				"uri":    uri,
				"txID":   txHash,
				"status": receipt.Status,
			})
			return
		}

		// Verify transaction data.
		fetchedTx, _, err := client.TransactionByHash(ctx, txHash)
		if err != nil {
			w.log.Warn("failed to fetch C-chain transaction by hash",
				zap.String("uri", uri),
				zap.Stringer("txID", txHash),
				zap.Error(err),
			)
			if errors.Is(err, ethereum.NotFound) {
				assert.Unreachable("C-chain transaction not found after receipt confirmed", map[string]any{
					"worker": w.id,
					"uri":    uri,
					"txID":   txHash,
				})
			}
			return
		}

		if fetchedTx.Hash() != sentTx.Hash() {
			w.log.Error("C-chain transaction hash mismatch",
				zap.String("uri", uri),
				zap.Stringer("expected", sentTx.Hash()),
				zap.Stringer("actual", fetchedTx.Hash()),
			)
			assert.Unreachable("C-chain transaction hash mismatch", map[string]any{
				"worker":   w.id,
				"uri":      uri,
				"expected": sentTx.Hash(),
				"actual":   fetchedTx.Hash(),
			})
			return
		}

		if fetchedTx.Nonce() != sentTx.Nonce() {
			w.log.Error("C-chain transaction nonce mismatch",
				zap.String("uri", uri),
				zap.Stringer("txID", txHash),
				zap.Uint64("expected", sentTx.Nonce()),
				zap.Uint64("actual", fetchedTx.Nonce()),
			)
			assert.Unreachable("C-chain transaction nonce mismatch", map[string]any{
				"worker":   w.id,
				"uri":      uri,
				"txID":     txHash,
				"expected": sentTx.Nonce(),
				"actual":   fetchedTx.Nonce(),
			})
			return
		}

		if fetchedTx.Value().Cmp(sentTx.Value()) != 0 {
			w.log.Error("C-chain transaction value mismatch",
				zap.String("uri", uri),
				zap.Stringer("txID", txHash),
				zap.Stringer("expected", sentTx.Value()),
				zap.Stringer("actual", fetchedTx.Value()),
			)
			assert.Unreachable("C-chain transaction value mismatch", map[string]any{
				"worker":   w.id,
				"uri":      uri,
				"txID":     txHash,
				"expected": sentTx.Value(),
				"actual":   fetchedTx.Value(),
			})
			return
		}

		if fetchedTx.To() == nil || *fetchedTx.To() != *sentTx.To() {
			w.log.Error("C-chain transaction recipient mismatch",
				zap.String("uri", uri),
				zap.Stringer("txID", txHash),
				zap.Stringer("expected", sentTx.To()),
				zap.Stringer("actual", fetchedTx.To()),
			)
			assert.Unreachable("C-chain transaction recipient mismatch", map[string]any{
				"worker":   w.id,
				"uri":      uri,
				"txID":     txHash,
				"expected": sentTx.To(),
				"actual":   fetchedTx.To(),
			})
			return
		}

		if !bytes.Equal(fetchedTx.Data(), sentTx.Data()) {
			w.log.Error("C-chain transaction data mismatch",
				zap.String("uri", uri),
				zap.Stringer("txID", txHash),
				zap.Int("expectedLen", len(sentTx.Data())),
				zap.Int("actualLen", len(fetchedTx.Data())),
			)
			assert.Unreachable("C-chain transaction data mismatch", map[string]any{
				"worker":      w.id,
				"uri":         uri,
				"txID":        txHash,
				"expectedLen": len(sentTx.Data()),
				"actualLen":   len(fetchedTx.Data()),
			})
			return
		}

		// Verify block data consistency.
		blockByNum, err := client.BlockByNumber(ctx, receipt.BlockNumber)
		if err != nil {
			w.log.Warn("failed to fetch block by number",
				zap.String("uri", uri),
				zap.Stringer("txID", txHash),
				zap.Stringer("blockNumber", receipt.BlockNumber),
				zap.Error(err),
			)
			if errors.Is(err, ethereum.NotFound) {
				assert.Unreachable("block not found by number after receipt confirmed", map[string]any{
					"worker":      w.id,
					"uri":         uri,
					"txID":        txHash,
					"blockNumber": receipt.BlockNumber,
				})
			}
			return
		}

		blockByHash, err := client.BlockByHash(ctx, receipt.BlockHash)
		if err != nil {
			w.log.Warn("failed to fetch block by hash",
				zap.String("uri", uri),
				zap.Stringer("txID", txHash),
				zap.Stringer("blockHash", receipt.BlockHash),
				zap.Error(err),
			)
			if errors.Is(err, ethereum.NotFound) {
				assert.Unreachable("block not found by hash after receipt confirmed", map[string]any{
					"worker":    w.id,
					"uri":       uri,
					"txID":      txHash,
					"blockHash": receipt.BlockHash,
				})
			}
			return
		}

		if blockByNum.Hash() != blockByHash.Hash() {
			w.log.Error("block hash mismatch between BlockByNumber and BlockByHash",
				zap.String("uri", uri),
				zap.Stringer("txID", txHash),
				zap.Stringer("byNumber", blockByNum.Hash()),
				zap.Stringer("byHash", blockByHash.Hash()),
			)
			assert.Unreachable("block hash mismatch between BlockByNumber and BlockByHash", map[string]any{
				"worker":   w.id,
				"uri":      uri,
				"txID":     txHash,
				"byNumber": blockByNum.Hash(),
				"byHash":   blockByHash.Hash(),
			})
			return
		}

		if blockByNum.Hash() != receipt.BlockHash {
			w.log.Error("block hash does not match receipt",
				zap.String("uri", uri),
				zap.Stringer("txID", txHash),
				zap.Stringer("blockHash", blockByNum.Hash()),
				zap.Stringer("receiptBlockHash", receipt.BlockHash),
			)
			assert.Unreachable("block hash does not match receipt", map[string]any{
				"worker":           w.id,
				"uri":              uri,
				"txID":             txHash,
				"blockHash":        blockByNum.Hash(),
				"receiptBlockHash": receipt.BlockHash,
			})
			return
		}

		blockTxs := blockByNum.Transactions()
		found := false
		for _, blockTx := range blockTxs {
			if blockTx.Hash() == txHash {
				found = true
				break
			}
		}
		if !found {
			w.log.Error("transaction not found in block",
				zap.String("uri", uri),
				zap.Stringer("txID", txHash),
				zap.Stringer("blockNumber", blockByNum.Number()),
				zap.Stringer("blockHash", blockByNum.Hash()),
				zap.Int("blockTxCount", len(blockTxs)),
			)
			assert.Unreachable("transaction not found in block", map[string]any{
				"worker":       w.id,
				"uri":          uri,
				"txID":         txHash,
				"blockNumber":  blockByNum.Number(),
				"blockHash":    blockByNum.Hash(),
				"blockTxCount": len(blockTxs),
			})
			return
		}

		w.log.Info("confirmed C-chain transaction",
			zap.Stringer("txID", txHash),
			zap.String("uri", uri),
		)
	}

	w.log.Info("confirmed C-chain transaction on all nodes",
		zap.Stringer("txID", txHash),
	)
	assert.Reachable("confirmed C-chain transaction on all nodes", map[string]any{
		"worker": w.id,
		"txID":   txHash,
	})
}

// transferCChainFunds sends a simple EVM value transfer to fund a C-chain worker.
func transferCChainFunds(ctx context.Context, client *ethclient.Client, key *ecdsa.PrivateKey, recipientAddress ethcommon.Address, txAmount uint64, log logging.Logger) error {
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch chainID: %w", err)
	}
	acceptedNonce, err := client.AcceptedNonceAt(ctx, crypto.PubkeyToAddress(key.PublicKey))
	if err != nil {
		return fmt.Errorf("failed to fetch accepted nonce: %w", err)
	}
	gasTipCap, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch suggested gas tip: %w", err)
	}
	gasFeeCap, err := client.EstimateBaseFee(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch estimated base fee: %w", err)
	}
	signer := types.LatestSignerForChainID(chainID)

	tx, err := types.SignNewTx(key, signer, &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     acceptedNonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       ethparams.TxGas,
		To:        &recipientAddress,
		Value:     big.NewInt(int64(txAmount)),
	})
	if err != nil {
		return fmt.Errorf("failed to sign transaction: %w", err)
	}

	log.Info("sending C-chain funding transaction",
		zap.Stringer("txID", tx.Hash()),
		zap.Uint64("nonce", acceptedNonce),
	)
	if err := client.SendTransaction(ctx, tx); err != nil {
		return fmt.Errorf("failed to send transaction: %w", err)
	}

	if _, err := bind.WaitMined(ctx, client, tx); err != nil {
		return fmt.Errorf("failed to wait for receipt: %w", err)
	}
	log.Info("confirmed C-chain funding transaction",
		zap.Stringer("txID", tx.Hash()),
	)

	return nil
}
