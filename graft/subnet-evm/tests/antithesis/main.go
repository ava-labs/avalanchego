// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"fmt"
	"math/big"
	"path/filepath"
	"runtime"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/antithesishq/antithesis-sdk-go/lifecycle"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/ethclient"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/tests/utils"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/logging"

	ago_tests "github.com/ava-labs/avalanchego/tests"
	timerpkg "github.com/ava-labs/avalanchego/utils/timer"
	ethparams "github.com/ava-labs/libevm/params"
)

const NumKeys = 5

func main() {
	logger := ago_tests.NewDefaultLogger("")
	tc := antithesis.NewInstrumentedTestContext(logger)
	defer tc.RecoverAndExit()
	require := require.New(tc)

	c := antithesis.NewConfigWithSubnets(
		tc,
		// TODO(marun) Centralize network configuration for all test types
		utils.NewTmpnetNetwork(
			"antithesis-subnet-evm",
			nil,
			tmpnet.FlagsMap{},
		),
		func(nodes ...*tmpnet.Node) []*tmpnet.Subnet {
			_, thisFile, _, _ := runtime.Caller(0)
			genesisPath := filepath.Join(filepath.Dir(thisFile), "..", "load", "genesis", "genesis.json")
			return []*tmpnet.Subnet{
				utils.NewTmpnetSubnet("subnet-evm", genesisPath, utils.DefaultChainConfig, nodes...),
			}
		},
	)
	ctx := ago_tests.DefaultNotifyContext(c.Duration, tc.DeferCleanup)

	// Ensure contexts sourced from the test context use the notify context as their parent
	tc.SetDefaultContextParent(ctx)

	require.Len(c.ChainIDs, 1)
	logger.Info("Starting testing",
		zap.Strings("chainIDs", c.ChainIDs),
	)
	chainID, err := ids.FromString(c.ChainIDs[0])
	require.NoError(err, "failed to parse chainID")

	genesisClient, err := ethclient.Dial(getChainURI(c.URIs[0], chainID.String()))
	require.NoError(err, "failed to dial chain")
	genesisKey := tmpnet.HardhatKey.ToECDSA()
	genesisWorkload := &workload{
		id:     0,
		log:    ago_tests.NewDefaultLogger(fmt.Sprintf("worker %d", 0)),
		client: genesisClient,
		key:    genesisKey,
	}

	workloads := make([]*workload, NumKeys)
	workloads[0] = genesisWorkload

	initialAmount := uint64(1_000_000_000_000_000)
	for i := 1; i < NumKeys; i++ {
		key, err := crypto.ToECDSA(crypto.Keccak256([]byte{uint8(i)}))
		require.NoError(err, "failed to generate key")

		require.NoError(transferFunds(ctx, genesisClient, genesisKey, crypto.PubkeyToAddress(key.PublicKey), initialAmount, logger))

		client, err := ethclient.Dial(getChainURI(c.URIs[i%len(c.URIs)], chainID.String()))
		require.NoError(err, "failed to dial chain")

		workloads[i] = &workload{
			id:     i,
			log:    ago_tests.NewDefaultLogger(fmt.Sprintf("worker %d", i)),
			client: client,
			key:    key,
		}
	}

	lifecycle.SetupComplete(map[string]any{
		"msg":        "initialized workers",
		"numWorkers": NumKeys,
	})

	for _, w := range workloads[1:] {
		go w.run(ctx)
	}
	genesisWorkload.run(ctx)
}

type workload struct {
	id     int
	client ethclient.Client
	log    logging.Logger
	key    *ecdsa.PrivateKey
}

// newTestContext returns a test context that ensures that log output and assertions are
// associated with this worker.
func (w *workload) newTestContext(ctx context.Context) *ago_tests.SimpleTestContext {
	return antithesis.NewInstrumentedTestContextWithArgs(
		ctx,
		w.log,
		map[string]any{
			"worker": w.id,
		},
	)
}

func (w *workload) run(ctx context.Context) {
	timer := timerpkg.StoppedTimer()

	tc := w.newTestContext(ctx)
	// Any assertion failure from this test context will result in process exit due to the
	// panic being rethrown. This ensures that failures in test setup are fatal.
	defer tc.RecoverAndRethrow()
	require := require.New(tc)

	balance, err := w.client.BalanceAt(ctx, crypto.PubkeyToAddress(w.key.PublicKey), nil)
	require.NoError(err, "failed to fetch balance")
	assert.Reachable("worker starting", map[string]any{
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

func (w *workload) executeTest(ctx context.Context) {
	// TODO(marun) What should this value be?
	txAmount := uint64(10000)
	// TODO(marun) Exercise a wider variety of transactions
	recipientEthAddress := crypto.PubkeyToAddress(w.key.PublicKey)
	err := transferFunds(ctx, w.client, w.key, recipientEthAddress, txAmount, w.log)
	if err != nil {
		// Log the error and continue since the problem may be
		// transient. require.NoError is only for errors that should stop
		// execution.
		w.log.Info("failed to transfer funds",
			zap.Error(err),
		)
	}
}

func getChainURI(nodeURI string, blockchainID string) string {
	return fmt.Sprintf("%s/ext/bc/%s/rpc", nodeURI, blockchainID)
}

func transferFunds(ctx context.Context, client ethclient.Client, key *ecdsa.PrivateKey, recipientAddress common.Address, txAmount uint64, log logging.Logger) error {
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
		return fmt.Errorf("failed to format transaction: %w", err)
	}

	log.Info("sending transaction", zap.Stringer("txID", tx.Hash()), zap.Uint64("nonce", acceptedNonce))
	err = client.SendTransaction(ctx, tx)
	if err != nil {
		return fmt.Errorf("failed to send transaction: %w", err)
	}

	log.Info("waiting for acceptance of transaction", zap.Stringer("txID", tx.Hash()))
	if _, err := bind.WaitMined(ctx, client, tx); err != nil {
		return fmt.Errorf("failed to wait for receipt: %w", err)
	}
	log.Info("confirmed acceptance of transaction", zap.Stringer("txID", tx.Hash()))

	return nil
}
