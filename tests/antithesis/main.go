// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"path/filepath"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/antithesishq/antithesis-sdk-go/lifecycle"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"

	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/tests"
	"github.com/ava-labs/subnet-evm/tests/utils"

	ago_tests "github.com/ava-labs/avalanchego/tests"
	timerpkg "github.com/ava-labs/avalanchego/utils/timer"
)

const NumKeys = 5

func main() {
	tc := ago_tests.NewTestContext()
	defer tc.Cleanup()
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
			repoRootPath := tests.GetRepoRootPath("tests/antithesis")
			genesisPath := filepath.Join(repoRootPath, "tests/load/genesis/genesis.json")
			return []*tmpnet.Subnet{
				utils.NewTmpnetSubnet("subnet-evm", genesisPath, utils.DefaultChainConfig, nodes...),
			}
		},
	)
	ctx := ago_tests.DefaultNotifyContext(c.Duration, tc.DeferCleanup)

	require.Len(c.ChainIDs, 1)
	log.Printf("CHAIN IDS: %v", c.ChainIDs)
	chainID, err := ids.FromString(c.ChainIDs[0])
	require.NoError(err, "failed to parse chainID")

	genesisClient, err := ethclient.Dial(getChainURI(c.URIs[0], chainID.String()))
	require.NoError(err, "failed to dial chain")
	genesisKey := tmpnet.HardhatKey.ToECDSA()
	genesisWorkload := &workload{
		id:     0,
		client: genesisClient,
		key:    genesisKey,
		uris:   c.URIs,
	}

	workloads := make([]*workload, NumKeys)
	workloads[0] = genesisWorkload

	initialAmount := uint64(1_000_000_000_000_000)
	for i := 1; i < NumKeys; i++ {
		key, err := crypto.ToECDSA(crypto.Keccak256([]byte{uint8(i)}))
		require.NoError(err, "failed to generate key")

		require.NoError(transferFunds(ctx, genesisClient, genesisKey, crypto.PubkeyToAddress(key.PublicKey), initialAmount))

		client, err := ethclient.Dial(getChainURI(c.URIs[i%len(c.URIs)], chainID.String()))
		require.NoError(err, "failed to dial chain")

		workloads[i] = &workload{
			id:     i,
			client: client,
			key:    key,
			uris:   c.URIs,
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
	key    *ecdsa.PrivateKey
	uris   []string
}

func (w *workload) run(ctx context.Context) {
	timer := timerpkg.StoppedTimer()

	tc := ago_tests.NewTestContext()
	defer tc.Cleanup()
	require := require.New(tc)

	balance, err := w.client.BalanceAt(ctx, crypto.PubkeyToAddress(w.key.PublicKey), nil)
	require.NoError(err, "failed to fetch balance")
	assert.Reachable("worker starting", map[string]any{
		"worker":  w.id,
		"balance": balance,
	})

	// TODO(marun) What should this value be?
	txAmount := uint64(10000)
	for {
		// TODO(marun) Exercise a wider variety of transactions
		recipientEthAddress := crypto.PubkeyToAddress(w.key.PublicKey)
		err := transferFunds(ctx, w.client, w.key, recipientEthAddress, txAmount)
		if err != nil {
			// Log the error and continue since the problem may be
			// transient. require.NoError is only for errors that should stop
			// execution.
			log.Printf("failed to transfer funds: %s", err)
		}

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

func getChainURI(nodeURI string, blockchainID string) string {
	return fmt.Sprintf("%s/ext/bc/%s/rpc", nodeURI, blockchainID)
}

func transferFunds(ctx context.Context, client ethclient.Client, key *ecdsa.PrivateKey, recipientAddress common.Address, txAmount uint64) error {
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
		Gas:       params.TxGas,
		To:        &recipientAddress,
		Value:     big.NewInt(int64(txAmount)),
	})
	if err != nil {
		return fmt.Errorf("failed to format transaction: %w", err)
	}

	log.Printf("sending transaction with ID %s and nonce %d\n", tx.Hash(), acceptedNonce)
	err = client.SendTransaction(ctx, tx)
	if err != nil {
		return fmt.Errorf("failed to send transaction: %w", err)
	}

	log.Printf("waiting for acceptance of transaction with ID %s\n", tx.Hash())
	if _, err := bind.WaitMined(ctx, client, tx); err != nil {
		return fmt.Errorf("failed to wait for receipt: %v", err)
	}
	log.Printf("confirmed acceptance of transaction with ID %s\n", tx.Hash())

	return nil
}
