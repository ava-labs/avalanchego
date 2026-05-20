// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ava-labs/libevm/accounts/abi/bind"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/load/contracts"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	blockchainID                          = "C"
	defaultTargetBytes             int64  = 10 * 1024 * 1024
	defaultMinBootstrapHeight      uint64 = 300
	defaultBatchSize                      = 5
	defaultWritesPerTx             int64  = 250
	defaultLoadWriteSlots          int64  = 64
	defaultLoadModifySlots         int64  = 32
	defaultStateSyncMinBlocks      uint64 = 32
	defaultStateSyncCommitInterval uint64 = 16
	defaultStateHistory            uint64 = 128
	defaultPollingDelay                   = 2 * time.Second
)

var (
	defaultGasFeeCap   = big.NewInt(300_000_000_000)
	defaultGasTipCap   = big.NewInt(1_000_000_000)
	defaultTransferWei = big.NewInt(1)

	flagVars *e2e.FlagVars

	targetBytes             int64
	minBootstrapHeight      uint64
	batchSize               int
	writesPerTx             int64
	loadWriteSlots          int64
	loadModifySlots         int64
	stateSyncMinBlocks      uint64
	stateSyncCommitInterval uint64
)

type deployedContracts struct {
	trieAddress common.Address
	trie        *contracts.TrieStressTest
	loadAddress common.Address
	load        *contracts.LoadSimulator
}

type workloadSnapshot struct {
	trieArrayLength      *big.Int
	latestWriteValue     *big.Int
	latestModifyValue    *big.Int
	latestEmptySlot      *big.Int
	latestUnmodifiedSlot *big.Int
	transferRecipient    common.Address
	transferBalance      *big.Int
}

func init() {
	flagVars = e2e.RegisterFlags(
		e2e.WithDefaultOwner("avalanchego-msync-e2e"),
	)

	flag.Int64Var(
		&targetBytes,
		"target-bytes",
		defaultTargetBytes,
		"target growth in bytes for the measured node data before validating bootstrap; set to 0 to disable the size threshold",
	)
	flag.Uint64Var(
		&minBootstrapHeight,
		"min-bootstrap-height",
		defaultMinBootstrapHeight,
		"minimum C-Chain height required before validating bootstrap to ensure recent block backfill is exercised",
	)
	flag.IntVar(
		&batchSize,
		"batch-size",
		defaultBatchSize,
		"number of mixed-workload iterations to issue per measurement batch",
	)
	flag.Int64Var(
		&writesPerTx,
		"writes-per-tx",
		defaultWritesPerTx,
		"number of trie writes to perform per TrieStressTest transaction",
	)
	flag.Int64Var(
		&loadWriteSlots,
		"load-write-slots",
		defaultLoadWriteSlots,
		"number of LoadSimulator slots to populate per write transaction",
	)
	flag.Int64Var(
		&loadModifySlots,
		"load-modify-slots",
		defaultLoadModifySlots,
		"number of LoadSimulator slots to modify per modify transaction",
	)
	flag.Uint64Var(
		&stateSyncMinBlocks,
		"state-sync-min-blocks",
		defaultStateSyncMinBlocks,
		"minimum number of blocks ahead required for the bootstrap node to choose state sync",
	)
	flag.Uint64Var(
		&stateSyncCommitInterval,
		"state-sync-commit-interval",
		defaultStateSyncCommitInterval,
		"state sync summary interval to use for validator nodes and the bootstrap node",
	)

	flag.Parse()
}

func main() {
	log := tests.NewDefaultLogger("msync-e2e")
	tc := tests.NewTestContext(log)
	defer tc.RecoverAndExit()

	require := require.New(tc)
	require.GreaterOrEqual(targetBytes, int64(0), "target-bytes must be non-negative")
	require.Positive(minBootstrapHeight, "min-bootstrap-height must be positive")
	require.Positive(batchSize, "batch-size must be positive")
	require.Positive(writesPerTx, "writes-per-tx must be positive")
	require.Positive(loadWriteSlots, "load-write-slots must be positive")
	require.Positive(loadModifySlots, "load-modify-slots must be positive")
	require.Positive(stateSyncMinBlocks, "state-sync-min-blocks must be positive")
	require.Positive(stateSyncCommitInterval, "state-sync-commit-interval must be positive")

	network := newMerkleSyncNetwork(flagVars.NetworkOwner())
	require.NoError(configureNetwork(tc, network))
	registerNetworkCleanup(tc, network)

	generationNode := network.Nodes[0]
	servingNode := network.Nodes[1]
	require.NoError(startGenerationNode(tc, network, generationNode))

	pathsToMeasure := statePaths(generationNode)
	initialSize, err := totalSize(pathsToMeasure...)
	require.NoError(err)

	client := newWSClient(tc, []*tmpnet.Node{generationNode})
	chainID, err := client.ChainID(tc.DefaultContext())
	require.NoError(err)

	fundingKey := network.PreFundedKeys[0]
	transferRecipientKey := network.PreFundedKeys[1]
	transferRecipient := crypto.PubkeyToAddress(transferRecipientKey.ToECDSA().PublicKey)
	initialRecipientBalance, err := client.BalanceAt(tc.DefaultContext(), transferRecipient, nil)
	require.NoError(err)

	contracts := deployContracts(tc, client, chainID, fundingKey)
	issueAtomicExportTx(tc, network, fundingKey)
	snapshot := generateWorkload(tc, client, chainID, fundingKey, transferRecipient, contracts, pathsToMeasure, initialSize, initialRecipientBalance)

	require.NoError(stopNode(generationNode))
	require.NoError(copySharedState(generationNode, servingNode))
	require.NoError(startServingNetwork(tc, network, generationNode, servingNode))

	servingClient := newWSClient(tc, []*tmpnet.Node{generationNode})
	expectedSummaryHeight := refreshStateSummaries(tc, servingClient, chainID, fundingKey, transferRecipient)

	bootstrapNode := checkMerkleSyncBootstrap(tc, network)
	if bootstrapNode != nil {
		bootstrapClient := newWSClient(tc, []*tmpnet.Node{bootstrapNode})
		validatePostBootstrapState(tc, bootstrapClient, snapshot, contracts)
		validateMerkleSyncEvidence(tc, network, bootstrapNode, expectedSummaryHeight)

		// SimpleTestContext cleanup runs in registration order rather than LIFO,
		// so the network-level cleanup may stop this ephemeral node before the
		// bootstrap helper's cleanup runs. Clearing the URI avoids a best-effort
		// metrics snapshot against an already-stopped node during cleanup.
		bootstrapNode.URI = ""
	}
}

func newMerkleSyncNetwork(owner string) *tmpnet.Network {
	return &tmpnet.Network{
		UUID:                tmpnet.NewDefaultNetwork(owner).UUID,
		Owner:               owner,
		Nodes:               tmpnet.NewNodesOrPanic(2),
		PrimaryChainConfigs: newMerkleSyncPrimaryChainConfigs(),
	}
}

func configureNetwork(tc tests.TestContext, network *tmpnet.Network) error {
	runtimeConfig, err := flagVars.NodeRuntimeConfig()
	if err != nil {
		return err
	}
	network.DefaultRuntimeConfig = *runtimeConfig
	if err := network.EnsureDefaultConfig(tc.DefaultContext(), tc.Log()); err != nil {
		return err
	}
	return network.Create(flagVars.RootNetworkDir())
}

func registerNetworkCleanup(tc tests.TestContext, network *tmpnet.Network) {
	require := require.New(tc)
	shutdownDelay := flagVars.NetworkShutdownDelay()
	tc.DeferCleanup(func() {
		if shutdownDelay > 0 {
			time.Sleep(shutdownDelay)
		}
		ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
		defer cancel()
		require.NoError(network.Stop(ctx))
	})
}

func statePaths(node *tmpnet.Node) []string {
	return []string{
		filepath.Join(node.DataDir, "db"),
		filepath.Join(node.DataDir, "chainData"),
	}
}

func startGenerationNode(tc tests.TestContext, network *tmpnet.Network, node *tmpnet.Node) error {
	node.Flags[config.SybilProtectionEnabledKey] = "false"
	return startNodes(tc, network, node)
}

func startServingNetwork(tc tests.TestContext, network *tmpnet.Network, generationNode *tmpnet.Node, servingNode *tmpnet.Node) error {
	delete(generationNode.Flags, config.SybilProtectionEnabledKey)
	return startNodes(tc, network, generationNode, servingNode)
}

func startNodes(tc tests.TestContext, network *tmpnet.Network, nodes ...*tmpnet.Node) error {
	for _, node := range nodes {
		if err := network.StartNode(tc.DefaultContext(), node); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
	defer cancel()
	return tmpnet.WaitForHealthyNodes(ctx, tc.Log(), nodes)
}

func stopNode(node *tmpnet.Node) error {
	ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
	defer cancel()
	return node.Stop(ctx)
}

func copySharedState(sourceNode *tmpnet.Node, targetNode *tmpnet.Node) error {
	for _, relativePath := range []string{"db", "chainData"} {
		sourcePath := filepath.Join(sourceNode.DataDir, relativePath)
		if _, err := os.Stat(sourcePath); err != nil {
			return fmt.Errorf("missing expected shared state path %q: %w", sourcePath, err)
		}
		targetPath := filepath.Join(targetNode.DataDir, relativePath)
		if err := os.RemoveAll(targetPath); err != nil {
			return err
		}
		if err := copyDir(sourcePath, targetPath); err != nil {
			return err
		}
	}
	return nil
}

func copyDir(sourceRoot string, targetRoot string) error {
	return filepath.WalkDir(sourceRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		relPath, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetRoot, relPath)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer sourceFile.Close()

		targetFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		defer targetFile.Close()

		_, err = io.Copy(targetFile, sourceFile)
		return err
	})
}

func refreshStateSummaries(
	tc tests.TestContext,
	client *ethclient.Client,
	chainID *big.Int,
	fundingKey *secp256k1.PrivateKey,
	transferRecipient common.Address,
) uint64 {
	require := require.New(tc)

	headBlock, err := client.BlockNumber(tc.DefaultContext())
	require.NoError(err)
	blocksUntilSummary := stateSyncCommitInterval - (headBlock % stateSyncCommitInterval)
	if blocksUntilSummary == 0 {
		blocksUntilSummary = stateSyncCommitInterval
	}

	for range blocksUntilSummary {
		issueTransfer(tc, client, chainID, fundingKey, transferRecipient, big.NewInt(0))
	}

	updatedHeadBlock, err := client.BlockNumber(tc.DefaultContext())
	require.NoError(err)
	require.Greater(updatedHeadBlock, headBlock, "expected post-restart summary refresh to advance the head block")
	require.Zero(updatedHeadBlock%stateSyncCommitInterval, "expected refreshed head block to land on a state sync summary boundary")
	tc.Log().Info("forced post-restart blocks to produce a fresh state summary",
		zap.Uint64("initialHeadBlock", headBlock),
		zap.Uint64("updatedHeadBlock", updatedHeadBlock),
		zap.Uint64("stateSyncCommitInterval", stateSyncCommitInterval),
	)
	return updatedHeadBlock
}

func newMerkleSyncPrimaryChainConfigs() map[string]tmpnet.ConfigMap {
	primaryChainConfigs := tmpnet.DefaultChainConfigs()
	if _, ok := primaryChainConfigs[blockchainID]; !ok {
		primaryChainConfigs[blockchainID] = make(tmpnet.ConfigMap)
	}

	maps.Copy(primaryChainConfigs[blockchainID], tmpnet.ConfigMap{
		"state-scheme":               "firewood",
		"snapshot-cache":             0,
		"populate-missing-tries":     nil,
		"pruning-enabled":            true,
		"state-sync-enabled":         false,
		"state-sync-commit-interval": stateSyncCommitInterval,
		"commit-interval":            stateSyncCommitInterval,
		"state-history":              defaultStateHistory,
	})
	return primaryChainConfigs
}

func newWSClient(tc tests.TestContext, nodes []*tmpnet.Node) *ethclient.Client {
	require := require.New(tc)
	wsURIs, err := tmpnet.GetNodeWebsocketURIs(nodes, blockchainID)
	require.NoError(err)
	if len(wsURIs) == 0 {
		require.Len(nodes, 1)
		uri := strings.Replace(nodes[0].GetAccessibleURI(), "http://", "ws://", 1)
		uri = strings.Replace(uri, "https://", "wss://", 1)
		wsURIs = []string{uri + "/ext/bc/" + blockchainID + "/ws"}
	}

	client, err := ethclient.Dial(wsURIs[0])
	require.NoError(err)
	return client
}

func deployContracts(
	tc tests.TestContext,
	client *ethclient.Client,
	chainID *big.Int,
	fundingKey *secp256k1.PrivateKey,
) deployedContracts {
	require := require.New(tc)
	txOpts, err := newTxOpts(tc, chainID, fundingKey)
	require.NoError(err)

	trieAddress, trieTx, trieContract, err := contracts.DeployTrieStressTest(txOpts, client)
	require.NoError(err)
	_, err = bind.WaitDeployed(tc.DefaultContext(), client, trieTx)
	require.NoError(err)

	txOpts, err = newTxOpts(tc, chainID, fundingKey)
	require.NoError(err)
	loadAddress, loadTx, loadContract, err := contracts.DeployLoadSimulator(txOpts, client)
	require.NoError(err)
	_, err = bind.WaitDeployed(tc.DefaultContext(), client, loadTx)
	require.NoError(err)

	tc.Log().Info("deployed contracts for merkle sync workload",
		zap.Stringer("trieStressAddress", trieAddress),
		zap.Stringer("trieStressTxID", trieTx.Hash()),
		zap.Stringer("loadSimulatorAddress", loadAddress),
		zap.Stringer("loadSimulatorTxID", loadTx.Hash()),
	)

	return deployedContracts{
		trieAddress: trieAddress,
		trie:        trieContract,
		loadAddress: loadAddress,
		load:        loadContract,
	}
}

func generateWorkload(
	tc tests.TestContext,
	client *ethclient.Client,
	chainID *big.Int,
	fundingKey *secp256k1.PrivateKey,
	transferRecipient common.Address,
	contracts deployedContracts,
	pathsToMeasure []string,
	initialSize int64,
	initialRecipientBalance *big.Int,
) workloadSnapshot {
	require := require.New(tc)

	var (
		totalMixedIterations int
		totalTrieWrites      int64
		totalLoadWrites      int64
		latestWriteValue     = big.NewInt(0)
		latestModifyValue    = big.NewInt(0)
		transferTotal        = new(big.Int)
		lastBlockNumber      uint64
	)

	tc.By("generating mixed Firewood-backed workload until bootstrap thresholds are reached", func() {
		for {
			currentSize, err := totalSize(pathsToMeasure...)
			require.NoError(err)
			delta := currentSize - initialSize

			headBlock, err := client.BlockNumber(tc.DefaultContext())
			require.NoError(err)

			heightReady := headBlock >= minBootstrapHeight
			sizeReady := targetBytes == 0 || delta >= targetBytes
			workloadReady := totalMixedIterations > 0
			if heightReady && sizeReady && workloadReady {
				tc.Log().Info("reached merkle sync workload targets",
					zap.Uint64("targetHeight", minBootstrapHeight),
					zap.Uint64("headBlock", headBlock),
					zap.Int64("targetBytes", targetBytes),
					zap.Int64("initialBytes", initialSize),
					zap.Int64("currentBytes", currentSize),
					zap.Int64("deltaBytes", delta),
					zap.Int("totalMixedIterations", totalMixedIterations),
					zap.Int64("totalTrieWrites", totalTrieWrites),
					zap.Int64("totalLoadWrites", totalLoadWrites),
				)
				break
			}

			for range batchSize {
				iteration := totalMixedIterations + 1
				latestWriteValue = big.NewInt(int64(iteration))
				latestModifyValue = big.NewInt(int64(1_000_000 + iteration))

				lastBlockNumber = issueContractTx(tc, client, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
					return contracts.trie.WriteValues(txOpts, big.NewInt(writesPerTx))
				}, chainID, fundingKey)
				totalTrieWrites += writesPerTx

				lastBlockNumber = issueContractTx(tc, client, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
					return contracts.load.Write(txOpts, big.NewInt(loadWriteSlots), latestWriteValue)
				}, chainID, fundingKey)
				totalLoadWrites += loadWriteSlots

				if totalLoadWrites >= loadModifySlots {
					lastBlockNumber = issueContractTx(tc, client, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
						return contracts.load.Modify(txOpts, big.NewInt(loadModifySlots), latestModifyValue)
					}, chainID, fundingKey)
				}

				lastBlockNumber = issueTransfer(tc, client, chainID, fundingKey, transferRecipient, defaultTransferWei)
				transferTotal.Add(transferTotal, defaultTransferWei)
				totalMixedIterations++
			}

			time.Sleep(defaultPollingDelay)

			currentSize, err = totalSize(pathsToMeasure...)
			require.NoError(err)
			headBlock, err = client.BlockNumber(tc.DefaultContext())
			require.NoError(err)
			tc.Log().Info("measured merkle sync workload progress",
				zap.Uint64("targetHeight", minBootstrapHeight),
				zap.Uint64("headBlock", headBlock),
				zap.Int64("targetBytes", targetBytes),
				zap.Int64("initialBytes", initialSize),
				zap.Int64("currentBytes", currentSize),
				zap.Int64("deltaBytes", currentSize-initialSize),
				zap.Int("totalMixedIterations", totalMixedIterations),
				zap.Int64("totalTrieWrites", totalTrieWrites),
				zap.Int64("totalLoadWrites", totalLoadWrites),
				zap.Int64("loadModifySlots", loadModifySlots),
				zap.Uint64("lastBlockNumber", lastBlockNumber),
			)
		}
	})

	return workloadSnapshot{
		trieArrayLength:      big.NewInt(totalTrieWrites),
		latestWriteValue:     new(big.Int).Set(latestWriteValue),
		latestModifyValue:    new(big.Int).Set(latestModifyValue),
		latestEmptySlot:      big.NewInt(2 + totalLoadWrites),
		latestUnmodifiedSlot: big.NewInt(2 + totalLoadWrites - 1),
		transferRecipient:    transferRecipient,
		transferBalance:      new(big.Int).Add(initialRecipientBalance, transferTotal),
	}
}

func issueContractTx(
	tc tests.TestContext,
	client *ethclient.Client,
	issue func(*bind.TransactOpts) (*types.Transaction, error),
	chainID *big.Int,
	fundingKey *secp256k1.PrivateKey,
) uint64 {
	require := require.New(tc)
	txOpts, err := newTxOpts(tc, chainID, fundingKey)
	require.NoError(err)

	tx, err := issue(txOpts)
	require.NoError(err)

	receipt, err := bind.WaitMined(tc.DefaultContext(), client, tx)
	require.NoError(err)
	require.Equal(types.ReceiptStatusSuccessful, receipt.Status)
	return receipt.BlockNumber.Uint64()
}

func issueTransfer(
	tc tests.TestContext,
	client *ethclient.Client,
	chainID *big.Int,
	fundingKey *secp256k1.PrivateKey,
	to common.Address,
	amount *big.Int,
) uint64 {
	require := require.New(tc)
	from := crypto.PubkeyToAddress(fundingKey.ToECDSA().PublicKey)
	nonce, err := client.PendingNonceAt(tc.DefaultContext(), from)
	require.NoError(err)

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		To:        &to,
		Gas:       21_000,
		GasFeeCap: new(big.Int).Set(defaultGasFeeCap),
		GasTipCap: new(big.Int).Set(defaultGasTipCap),
		Value:     new(big.Int).Set(amount),
	})
	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), fundingKey.ToECDSA())
	require.NoError(err)
	require.NoError(client.SendTransaction(tc.DefaultContext(), signedTx))

	receipt, err := bind.WaitMined(tc.DefaultContext(), client, signedTx)
	require.NoError(err)
	require.Equal(types.ReceiptStatusSuccessful, receipt.Status)
	return receipt.BlockNumber.Uint64()
}

func checkMerkleSyncBootstrap(tc tests.TestContext, network *tmpnet.Network) *tmpnet.Node {
	require := require.New(tc)
	tc.By("checking if Firewood merkle sync bootstrap is possible with the current network state")

	subnetIDs := make([]string, len(network.Subnets))
	for i, subnet := range network.Subnets {
		subnetIDs[i] = subnet.SubnetID.String()
	}
	flags := tmpnet.FlagsMap{
		config.TrackSubnetsKey: strings.Join(subnetIDs, ","),
	}

	chainConfigContent, err := newBootstrapChainConfigContent(network)
	require.NoError(err)
	flags[config.ChainConfigContentKey] = chainConfigContent

	node := tmpnet.NewEphemeralNode(flags)
	require.NoError(network.StartNode(tc.DefaultContext(), node))

	tc.DeferCleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
		defer cancel()
		require.NoError(node.Stop(ctx))
	})

	require.NoError(node.WaitForHealthy(tc.DefaultContext()))

	for _, validator := range network.Nodes {
		if validator.IsEphemeral {
			continue
		}
		healthy, err := validator.IsHealthy(tc.DefaultContext())
		require.NoError(err)
		require.True(healthy, "primary validator %s is not healthy", validator.NodeID)
	}

	return node
}

func newBootstrapChainConfigContent(network *tmpnet.Network) (string, error) {
	chainConfigs := map[string]chains.ChainConfig{}
	for alias, flags := range network.PrimaryChainConfigs {
		nodeFlags := maps.Clone(flags)
		if alias == blockchainID {
			maps.Copy(nodeFlags, tmpnet.ConfigMap{
				"state-scheme":               "firewood",
				"snapshot-cache":             0,
				"populate-missing-tries":     nil,
				"pruning-enabled":            true,
				"state-sync-enabled":         true,
				"state-sync-min-blocks":      stateSyncMinBlocks,
				"state-sync-commit-interval": stateSyncCommitInterval,
				"commit-interval":            stateSyncCommitInterval,
				"state-history":              defaultStateHistory,
			})
		}
		marshaledFlags, err := json.Marshal(nodeFlags)
		if err != nil {
			return "", err
		}
		chainConfigs[alias] = chains.ChainConfig{Config: marshaledFlags}
	}

	marshaledConfigs, err := json.Marshal(chainConfigs)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(marshaledConfigs), nil
}

func validatePostBootstrapState(
	tc tests.TestContext,
	client *ethclient.Client,
	snapshot workloadSnapshot,
	contracts deployedContracts,
) {
	require := require.New(tc)
	ctx := tc.DefaultContext()

	trieCode, err := client.CodeAt(ctx, contracts.trieAddress, nil)
	require.NoError(err)
	require.NotEmpty(trieCode, "TrieStressTest code should exist after bootstrap")

	loadCode, err := client.CodeAt(ctx, contracts.loadAddress, nil)
	require.NoError(err)
	require.NotEmpty(loadCode, "LoadSimulator code should exist after bootstrap")

	trieLength, err := client.StorageAt(ctx, contracts.trieAddress, storageSlotKey(0), nil)
	require.NoError(err)
	require.Zero(snapshot.trieArrayLength.Cmp(new(big.Int).SetBytes(trieLength)), "unexpected TrieStressTest array length")

	latestEmptySlot, err := client.StorageAt(ctx, contracts.loadAddress, storageSlotKey(1), nil)
	require.NoError(err)
	require.Zero(snapshot.latestEmptySlot.Cmp(new(big.Int).SetBytes(latestEmptySlot)), "unexpected latestEmptySlot value")

	modifiedSlot, err := client.StorageAt(ctx, contracts.loadAddress, storageSlotKey(2), nil)
	require.NoError(err)
	require.Zero(snapshot.latestModifyValue.Cmp(new(big.Int).SetBytes(modifiedSlot)), "unexpected modified storage value")

	latestWrittenSlot, err := client.StorageAt(ctx, contracts.loadAddress, storageSlotBig(snapshot.latestUnmodifiedSlot), nil)
	require.NoError(err)
	require.Zero(snapshot.latestWriteValue.Cmp(new(big.Int).SetBytes(latestWrittenSlot)), "unexpected latest written storage value")

	balance, err := client.BalanceAt(ctx, snapshot.transferRecipient, nil)
	require.NoError(err)
	require.Zero(snapshot.transferBalance.Cmp(balance), "unexpected recipient balance after bootstrap")
}

func issueAtomicExportTx(
	tc tests.TestContext,
	network *tmpnet.Network,
	senderKey *secp256k1.PrivateKey,
) {
	require := require.New(tc)
	nodeURIs := network.GetNodeURIs()
	require.NotEmpty(nodeURIs)

	recipientKey := e2e.NewPrivateKey(tc)
	keychain := secp256k1fx.NewKeychain(senderKey, recipientKey)
	wallet := e2e.NewWallet(tc, keychain, nodeURIs[0])
	xContext := wallet.X().Builder().Context()

	exportOutputs := []*secp256k1fx.TransferOutput{{
		Amt: units.Avax,
		OutputOwners: secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				keychain.Keys[0].Address(),
			},
		},
	}}

	_, err := wallet.C().IssueExportTx(
		xContext.BlockchainID,
		exportOutputs,
		tc.WithDefaultContext(),
	)
	require.NoError(err)

	tc.Log().Info("issued C-Chain export transaction to populate atomic trie",
		zap.Stringer("destinationChainID", xContext.BlockchainID),
		zap.Uint64("amount", units.Avax),
	)
}

func validateMerkleSyncEvidence(tc tests.TestContext, network *tmpnet.Network, bootstrapNode *tmpnet.Node, expectedSummaryHeight uint64) {
	require := require.New(tc)

	deadline := time.Now().Add(e2e.DefaultTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = checkMerkleSyncEvidence(tc, network, bootstrapNode, expectedSummaryHeight)
		if lastErr == nil {
			return
		}
		time.Sleep(defaultPollingDelay)
	}
	require.NoError(lastErr)
}

func checkMerkleSyncEvidence(tc tests.TestContext, network *tmpnet.Network, bootstrapNode *tmpnet.Node, expectedSummaryHeight uint64) error {
	bootstrapMetrics, err := tests.GetNodeMetrics(tc.DefaultContext(), bootstrapNode.URI)
	if err != nil {
		return err
	}
	firewoodRequests, ok := tests.GetMetricValue(bootstrapMetrics, "avalanche_evm_sync_firewood_sync_requests_made", prometheus.Labels{"chain": blockchainID})
	if !ok {
		return errors.New("expected bootstrap node firewood sync metric")
	}
	if firewoodRequests <= 0 {
		return errors.New("expected bootstrap node to make firewood proof requests")
	}

	validatorURIs := make([]string, 0, len(network.Nodes))
	for _, node := range network.Nodes {
		if node.IsEphemeral {
			continue
		}
		validatorURIs = append(validatorURIs, node.URI)
	}
	validatorMetrics, err := tests.GetNodesMetrics(tc.DefaultContext(), validatorURIs)
	if err != nil {
		return err
	}
	if sumMetric(validatorMetrics, "avalanche_evm_eth_code_request_count", prometheus.Labels{"chain": blockchainID}) <= 0 {
		return errors.New("expected validators to serve code sync requests")
	}
	if sumMetric(validatorMetrics, "avalanche_evm_eth_block_request_count", prometheus.Labels{"chain": blockchainID}) <= 0 {
		return errors.New("expected validators to serve block backfill requests")
	}

	bootstrapLogPath := filepath.Join(bootstrapNode.DataDir, "logs", "C.log")
	bootstrapLog, err := os.ReadFile(bootstrapLogPath)
	if err != nil {
		return err
	}
	bootstrapLogText := string(bootstrapLog)
	for _, expectedText := range []string{
		"Firewood state scheme is enabled",
		"state sync started",
		"accepted state summary",
		"Firewood EVM State Syncer",
		"Code Syncer",
		"Block Syncer",
		"all syncers completed successfully",
	} {
		if !strings.Contains(bootstrapLogText, expectedText) {
			return fmt.Errorf("expected bootstrap log evidence %q", expectedText)
		}
	}
	heightPattern := regexp.MustCompile(fmt.Sprintf(`name="(Block Syncer|Code Syncer|Firewood EVM State Syncer)".*height=%d`, expectedSummaryHeight))
	if !heightPattern.MatchString(bootstrapLogText) {
		return errors.New("expected bootstrap syncer log line at refreshed summary height")
	}
	if strings.Contains(bootstrapLogText, "last accepted too close to most recent syncable block, skipping state sync") {
		return errors.New("unexpected bootstrap log evidence for skipping state sync")
	}
	return nil
}

func sumMetric(allMetrics tests.NodesMetrics, metricName string, labels prometheus.Labels) float64 {
	var total float64
	for _, nodeMetrics := range allMetrics {
		value, ok := tests.GetMetricValue(nodeMetrics, metricName, labels)
		if ok {
			total += value
		}
	}
	return total
}

func storageSlotKey(slot uint64) common.Hash {
	return common.BigToHash(new(big.Int).SetUint64(slot))
}

func storageSlotBig(slotValue *big.Int) common.Hash {
	return common.BigToHash(slotValue)
}

func newTxOpts(tc tests.TestContext, chainID *big.Int, fundingKey *secp256k1.PrivateKey) (*bind.TransactOpts, error) {
	txOpts, err := bind.NewKeyedTransactorWithChainID(fundingKey.ToECDSA(), chainID)
	if err != nil {
		return nil, err
	}
	txOpts.Context = tc.DefaultContext()
	txOpts.GasFeeCap = new(big.Int).Set(defaultGasFeeCap)
	txOpts.GasTipCap = new(big.Int).Set(defaultGasTipCap)
	return txOpts, nil
}

func totalSize(paths ...string) (int64, error) {
	var total int64
	for _, path := range paths {
		size, err := dirSize(path)
		if err != nil {
			return 0, err
		}
		total += size
	}
	return total, nil
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return total, err
}
