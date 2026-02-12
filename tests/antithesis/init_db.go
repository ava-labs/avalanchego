// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package antithesis

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	ethparams "github.com/ava-labs/libevm/params"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/graft/coreth/accounts/abi/bind"
	"github.com/ava-labs/avalanchego/graft/coreth/ethclient"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

// getBootstrapVolumePath returns the expected path of the bootstrap node's
// docker compose db volume for the given target path.
func getBootstrapVolumePath(targetPath string) (string, error) {
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to convert target path to absolute path: %w", err)
	}
	return filepath.Join(absPath, "volumes", getServiceName(bootstrapIndex)), nil
}

// initBootstrapDB bootstraps a local process-based network, creates its
// subnets and chains, and copies the resulting db state from one of the nodes
// to the provided path. The path will be created if it does not already exist.
//
// When cchainBlockCount > 0, the network is kept running after bootstrap to
// generate the specified number of C-chain blocks before copying the database.
// This is used to pre-seed the database with c-chain blocks.
func initBootstrapDB(network *tmpnet.Network, destPath string, cchainBlockCount int) error {
	log := tests.NewDefaultLogger("")

	// Allow ~3s per block (C-chain block time is ~2s) plus base time for
	// network bootstrap and shutdown.
	timeout := 2 * time.Minute
	if cchainBlockCount > 0 {
		timeout += time.Duration(cchainBlockCount) * 3 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := tmpnet.BootstrapNewNetwork(ctx, log, network, ""); err != nil {
		return fmt.Errorf("failed to bootstrap network: %w", err)
	}

	if cchainBlockCount > 0 {
		log.Info("generating C-chain blocks for migration testing",
			zap.Int("targetBlocks", cchainBlockCount),
		)
		if err := generateCChainBlocks(ctx, network, cchainBlockCount, log); err != nil {
			// Stop the network even if block generation fails.
			_ = network.Stop(ctx)
			return fmt.Errorf("failed to generate C-chain blocks: %w", err)
		}
	}

	if err := network.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop network: %w", err)
	}

	return copyBootstrapDB(network.Nodes[0].DataDir, destPath)
}

// copyBootstrapDB copies the db directory from a node's data dir to the
// destination path used for docker compose volumes.
func copyBootstrapDB(nodeDataDir string, destPath string) error {
	sourcePath := filepath.Join(nodeDataDir, "db")
	if err := os.MkdirAll(destPath, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create db path %q: %w", destPath, err)
	}
	// TODO(marun) Replace with os.CopyFS once we upgrade to Go 1.23
	cmd := exec.Command("cp", "-r", sourcePath, destPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy bootstrap db from %q to %q: %w", sourcePath, destPath, err)
	}
	return nil
}

// generateCChainBlocks sends self-transfer transactions on the C-chain to
// produce the target number of blocks. It uses the pre-funded EWOQ key from
// the local genesis.
func generateCChainBlocks(ctx context.Context, network *tmpnet.Network, targetBlocks int, log logging.Logger) error {
	uri := network.Nodes[0].GetAccessibleURI()
	client, err := ethclient.Dial(fmt.Sprintf("%s/ext/bc/C/rpc", uri))
	if err != nil {
		return fmt.Errorf("failed to dial C-chain RPC: %w", err)
	}

	key := genesis.EWOQKey.ToECDSA()
	sender := crypto.PubkeyToAddress(key.PublicKey)

	chainID, err := client.ChainID(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch chainID: %w", err)
	}

	signer := types.LatestSignerForChainID(chainID)
	txAmount := big.NewInt(1) // minimal value, self-transfer

	for i := 0; i < targetBlocks; i++ {
		nonce, err := client.AcceptedNonceAt(ctx, sender)
		if err != nil {
			return fmt.Errorf("failed to fetch nonce at block %d: %w", i, err)
		}

		gasTipCap, err := client.SuggestGasTipCap(ctx)
		if err != nil {
			return fmt.Errorf("failed to fetch gas tip at block %d: %w", i, err)
		}

		gasFeeCap, err := client.EstimateBaseFee(ctx)
		if err != nil {
			return fmt.Errorf("failed to fetch base fee at block %d: %w", i, err)
		}

		tx, err := types.SignNewTx(key, signer, &types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			GasTipCap: gasTipCap,
			GasFeeCap: gasFeeCap,
			Gas:       ethparams.TxGas,
			To:        &sender,
			Value:     txAmount,
		})
		if err != nil {
			return fmt.Errorf("failed to sign tx at block %d: %w", i, err)
		}

		if err := client.SendTransaction(ctx, tx); err != nil {
			return fmt.Errorf("failed to send tx at block %d: %w", i, err)
		}

		if _, err := bind.WaitMined(ctx, client, tx); err != nil {
			return fmt.Errorf("failed to wait for tx at block %d: %w", i, err)
		}

		if (i+1)%100 == 0 {
			log.Info("generated C-chain blocks",
				zap.Int("count", i+1),
				zap.Int("target", targetBlocks),
			)
		}
	}

	log.Info("finished generating C-chain blocks",
		zap.Int("total", targetBlocks),
	)
	return nil
}
