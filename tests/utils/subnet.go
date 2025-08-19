// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/libevm/log"
	"github.com/go-cmd/cmd"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/plugin/evm"

	wallet "github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

type SubnetSuite struct {
	blockchainIDs map[string]string
	lock          sync.RWMutex
}

func (s *SubnetSuite) GetBlockchainID(alias string) string {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.blockchainIDs[alias]
}

func (s *SubnetSuite) SetBlockchainIDs(blockchainIDs map[string]string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.blockchainIDs = blockchainIDs
}

// CreateSubnetsSuite creates subnets for given [genesisFiles], and registers a before suite that starts an AvalancheGo process to use for the e2e tests.
// genesisFiles is a map of test aliases to genesis file paths.
func CreateSubnetsSuite(genesisFiles map[string]string) *SubnetSuite {
	require := require.New(ginkgo.GinkgoT())

	// Keep track of the AvalancheGo external bash script, it is null for most
	// processes except the first process that starts AvalancheGo
	var startCmd *cmd.Cmd

	// This is used to pass the blockchain IDs from the SynchronizedBeforeSuite() to the tests
	var globalSuite SubnetSuite

	// Our test suite runs in separate processes, ginkgo has
	// SynchronizedBeforeSuite() which runs once, and its return value is passed
	// over to each worker.
	//
	// Here an AvalancheGo node instance is started, and subnets are created for
	// each test case. Each test case has its own subnet, therefore all tests
	// can run in parallel without any issue.
	//
	_ = ginkgo.SynchronizedBeforeSuite(func() []byte {
		ctx, cancel := context.WithTimeout(context.Background(), BootAvalancheNodeTimeout)
		defer cancel()

		wd, err := os.Getwd()
		require.NoError(err)
		log.Info("Starting AvalancheGo node", "wd", wd)
		cmd, err := RunCommand("./scripts/run.sh")
		require.NoError(err)
		startCmd = cmd

		// Assumes that startCmd will launch a node with HTTP Port at [utils.DefaultLocalNodeURI]
		healthClient := health.NewClient(DefaultLocalNodeURI)
		healthy, err := health.AwaitReady(ctx, healthClient, HealthCheckTimeout, nil)
		require.NoError(err)
		require.True(healthy)
		log.Info("AvalancheGo node is healthy")

		blockchainIDs := make(map[string]string)
		for alias, file := range genesisFiles {
			blockchainIDs[alias] = CreateNewSubnet(ctx, file)
		}

		blockchainIDsBytes, err := json.Marshal(blockchainIDs)
		require.NoError(err)
		return blockchainIDsBytes
	}, func(ctx ginkgo.SpecContext, data []byte) {
		blockchainIDs := make(map[string]string)
		require.NoError(json.Unmarshal(data, &blockchainIDs))

		globalSuite.SetBlockchainIDs(blockchainIDs)
	})

	// SynchronizedAfterSuite() takes two functions, the first runs after each test suite is done and the second
	// function is executed once when all the tests are done. This function is used
	// to gracefully shutdown the AvalancheGo node.
	_ = ginkgo.SynchronizedAfterSuite(func() {}, func() {
		require.NotNil(startCmd)
		require.NoError(startCmd.Stop())
	})

	return &globalSuite
}

// CreateNewSubnet creates a new subnet and Subnet-EVM blockchain with the given genesis file.
// returns the ID of the new created blockchain.
func CreateNewSubnet(ctx context.Context, genesisFilePath string) string {
	require := require.New(ginkgo.GinkgoT())

	kc := secp256k1fx.NewKeychain(genesis.EWOQKey)

	// MakeWallet fetches the available UTXOs owned by [kc] on the network
	// that [LocalAPIURI] is hosting.
	wallet, err := wallet.MakeWallet(ctx, DefaultLocalNodeURI, kc, kc, wallet.WalletConfig{})
	require.NoError(err)

	pWallet := wallet.P()

	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			genesis.EWOQKey.PublicKey().Address(),
		},
	}

	wd, err := os.Getwd()
	require.NoError(err)
	log.Info("Reading genesis file", "filePath", genesisFilePath, "wd", wd)
	genesisBytes, err := os.ReadFile(genesisFilePath)
	require.NoError(err)

	log.Info("Creating new subnet")
	createSubnetTx, err := pWallet.IssueCreateSubnetTx(owner)
	require.NoError(err)

	genesis := &core.Genesis{}
	require.NoError(json.Unmarshal(genesisBytes, genesis))

	log.Info("Creating new Subnet-EVM blockchain", "genesis", genesis)
	createChainTx, err := pWallet.IssueCreateChainTx(
		createSubnetTx.ID(),
		genesisBytes,
		evm.ID,
		nil,
		"testChain",
	)
	require.NoError(err)
	createChainTxID := createChainTx.ID()

	// Confirm the new blockchain is ready by waiting for the readiness endpoint
	infoClient := info.NewClient(DefaultLocalNodeURI)
	bootstrapped, err := info.AwaitBootstrapped(ctx, infoClient, createChainTxID.String(), 2*time.Second)
	require.NoError(err)
	require.True(bootstrapped)

	// Return the blockchainID of the newly created blockchain
	return createChainTxID.String()
}

// GetDefaultChainURI returns the default chain URI for a given blockchainID
func GetDefaultChainURI(blockchainID string) string {
	return fmt.Sprintf("%s/ext/bc/%s/rpc", DefaultLocalNodeURI, blockchainID)
}

// GetFilesAndAliases returns a map of aliases to file paths in given [dir].
func GetFilesAndAliases(dir string) (map[string]string, error) {
	files, err := filepath.Glob(dir)
	if err != nil {
		return nil, err
	}
	aliasesToFiles := make(map[string]string)
	for _, file := range files {
		alias := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		aliasesToFiles[alias] = file
	}
	return aliasesToFiles, nil
}
