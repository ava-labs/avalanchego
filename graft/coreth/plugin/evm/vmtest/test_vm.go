// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmtest

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
	"github.com/ava-labs/coreth/plugin/evm/extension"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	commoneng "github.com/ava-labs/avalanchego/snow/engine/common"
)

var Schemes = []string{rawdb.HashScheme, customrawdb.FirewoodScheme}

type TestVMConfig struct {
	IsSyncing bool
	Fork      *upgradetest.Fork
	// If genesisJSON is empty, defaults to the genesis corresponding to the
	// fork.
	GenesisJSON string
	ConfigJSON  string
	// DB scheme, defaults to HashScheme
	Scheme string
}

type TestVMSuite struct {
	VM           commoneng.VM
	DB           *prefixdb.Database
	AtomicMemory *avalancheatomic.Memory
	AppSender    *enginetest.Sender
	Ctx          *snow.Context
}

// SetupTestVM initializes a VM for testing. It sets up the genesis and returns the
// issuer channel, database, atomic memory, app sender, and context.
// Expects the passed VM to be a uninitialized VM.
func SetupTestVM(t *testing.T, vm commoneng.VM, config TestVMConfig) *TestVMSuite {
	fork := upgradetest.Latest
	if config.Fork != nil {
		fork = *config.Fork
	}
	snowtCtx, dbManager, genesisBytes, m := SetupGenesis(t, fork)
	if len(config.GenesisJSON) != 0 {
		genesisBytes = []byte(config.GenesisJSON)
	}
	appSender := &enginetest.Sender{
		T:                 t,
		CantSendAppGossip: true,
		SendAppGossipF:    func(context.Context, commoneng.SendConfig, []byte) error { return nil },
	}

	scheme := config.Scheme
	if len(scheme) == 0 {
		scheme = rawdb.HashScheme
	}

	configJSON, err := OverrideSchemeConfig(scheme, config.ConfigJSON)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	err = vm.Initialize(
		ctx,
		snowtCtx,
		dbManager,
		genesisBytes,
		nil,
		[]byte(configJSON),
		nil,
		appSender,
	)
	require.NoError(t, err, "error initializing GenesisVM")

	if !config.IsSyncing {
		require.NoError(t, vm.SetState(ctx, snow.Bootstrapping))
		require.NoError(t, vm.SetState(ctx, snow.NormalOp))
	}

	return &TestVMSuite{
		VM:           vm,
		DB:           dbManager,
		AtomicMemory: m,
		AppSender:    appSender,
		Ctx:          snowtCtx,
	}
}

// ResetMetrics resets the vm avalanchego metrics, and allows
// for the VM to be re-initialized in tests.
func ResetMetrics(snowCtx *snow.Context) {
	snowCtx.Metrics = metrics.NewPrefixGatherer()
}

func OverrideSchemeConfig(scheme string, configJSON string) (string, error) {
	// If the scheme is not Firewood, return the configJSON as is
	if scheme != customrawdb.FirewoodScheme {
		return configJSON, nil
	}

	// Parse existing config into a map to preserve only non-zero values
	configMap := make(map[string]interface{})
	if len(configJSON) > 0 {
		if err := json.Unmarshal([]byte(configJSON), &configMap); err != nil {
			return "", err
		}
	}

	// Set Firewood-specific configuration flags (these will override any existing values)
	configMap["state-scheme"] = customrawdb.FirewoodScheme
	configMap["snapshot-cache"] = 0
	configMap["pruning-enabled"] = true
	configMap["state-sync-enabled"] = false
	configMap["metrics-expensive-enabled"] = false

	// Marshal back to JSON
	result, err := json.Marshal(configMap)
	return string(result), err
}

func IssueTxsAndBuild(txs []*types.Transaction, vm extension.InnerVM) (snowman.Block, error) {
	errs := vm.Ethereum().TxPool().AddRemotesSync(txs)
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("failed to add tx at index %d: %w", i, err)
		}
	}

	msg, err := vm.WaitForEvent(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to wait for event: %w", err)
	}
	if msg != commoneng.PendingTxs {
		return nil, fmt.Errorf("expected pending txs, got %v", msg)
	}

	block, err := vm.BuildBlock(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to build block with transaction: %w", err)
	}

	if err := block.Verify(context.Background()); err != nil {
		return nil, fmt.Errorf("block verification failed: %w", err)
	}

	return block, nil
}

func IssueTxsAndSetPreference(txs []*types.Transaction, vm extension.InnerVM) (snowman.Block, error) {
	block, err := IssueTxsAndBuild(txs, vm)
	if err != nil {
		return nil, err
	}

	if err := vm.SetPreference(context.Background(), block.ID()); err != nil {
		return nil, fmt.Errorf("failed to set preference: %w", err)
	}

	return block, nil
}
