// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	wallet "github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/plugin/evm"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/onsi/gomega"
)

// RunTestCMD runs a given test command with the given rpcURI
// It also waits for the test ping to succeed before running the test command
func RunTestCMD(testCMD *exec.Cmd, rpcURI string) {
	log.Info("Sleeping to wait for test ping", "rpcURI", rpcURI)
	client, err := NewEvmClient(rpcURI, 225, 2)
	gomega.Expect(err).Should(gomega.BeNil())

	bal, err := client.FetchBalance(context.Background(), common.HexToAddress(""))
	gomega.Expect(err).Should(gomega.BeNil())
	gomega.Expect(bal.Cmp(common.Big0)).Should(gomega.Equal(0))

	err = os.Setenv("RPC_URI", rpcURI)
	gomega.Expect(err).Should(gomega.BeNil())
	log.Info("Running test command", "cmd", testCMD.String())

	out, err := testCMD.CombinedOutput()
	fmt.Printf("\nCombined output:\n\n%s\n", string(out))
	if err != nil {
		fmt.Printf("\nErr: %s\n", err.Error())
	}
	gomega.Expect(err).Should(gomega.BeNil())
}

// CreateNewSubnet creates a new subnet and Subnet-EVM blockchain with the given genesis file.
// returns the ID of the new created blockchain.
func CreateNewSubnet(ctx context.Context, genesisFilePath string) string {
	kc := secp256k1fx.NewKeychain(genesis.EWOQKey)

	// NewWalletFromURI fetches the available UTXOs owned by [kc] on the network
	// that [LocalAPIURI] is hosting.
	wallet, err := wallet.NewWalletFromURI(ctx, DefaultLocalNodeURI, kc)
	gomega.Expect(err).Should(gomega.BeNil())

	pWallet := wallet.P()

	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			genesis.EWOQKey.PublicKey().Address(),
		},
	}

	wd, err := os.Getwd()
	gomega.Expect(err).Should(gomega.BeNil())
	log.Info("Reading genesis file", "filePath", genesisFilePath, "wd", wd)
	genesisBytes, err := os.ReadFile(genesisFilePath)
	gomega.Expect(err).Should(gomega.BeNil())

	log.Info("Creating new subnet")
	createSubnetTxID, err := pWallet.IssueCreateSubnetTx(owner)
	gomega.Expect(err).Should(gomega.BeNil())

	genesis := &core.Genesis{}
	err = json.Unmarshal(genesisBytes, genesis)
	gomega.Expect(err).Should(gomega.BeNil())

	log.Info("Creating new Subnet-EVM blockchain", "genesis", genesis)
	createChainTxID, err := pWallet.IssueCreateChainTx(
		createSubnetTxID,
		genesisBytes,
		evm.ID,
		nil,
		"testChain",
	)
	gomega.Expect(err).Should(gomega.BeNil())

	// Confirm the new blockchain is ready by waiting for the readiness endpoint
	infoClient := info.NewClient(DefaultLocalNodeURI)
	bootstrapped, err := info.AwaitBootstrapped(ctx, infoClient, createChainTxID.String(), 2*time.Second)
	gomega.Expect(err).Should(gomega.BeNil())
	gomega.Expect(bootstrapped).Should(gomega.BeTrue())

	// Return the blockchainID of the newly created blockchain
	return createChainTxID.String()
}

// GetDefaultChainURI returns the default chain URI for a given blockchainID
func GetDefaultChainURI(blockchainID string) string {
	return fmt.Sprintf("%s/ext/bc/%s/rpc", DefaultLocalNodeURI, blockchainID)
}

// RunDefaultHardhatTests runs the hardhat tests on a given blockchain ID
// with default parameters. Default parameters are:
// 1. Hardhat contract environment is located at ./contracts
// 2. Hardhat test file is located at ./contracts/test/<test>.ts
// 3. npx is available in the ./contracts directory
func RunDefaultHardhatTests(ctx context.Context, blockchainID string, test string) {
	chainURI := GetDefaultChainURI(blockchainID)
	log.Info("Executing HardHat tests on a new blockchain", "blockchainID", blockchainID, "test", test)
	log.Info("Using subnet", "ChainURI", chainURI)

	cmdPath := "./contracts"
	// test path is relative to the cmd path
	testPath := fmt.Sprintf("./test/%s.ts", test)
	cmd := exec.Command("npx", "hardhat", "test", testPath, "--network", "local")
	cmd.Dir = cmdPath

	RunTestCMD(cmd, chainURI)
}
