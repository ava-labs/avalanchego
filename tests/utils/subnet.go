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

func RunHardhatTests(test string, rpcURI string) {
	log.Info("Sleeping to wait for test ping", "rpcURI", rpcURI)
	client, err := NewEvmClient(rpcURI, 225, 2)
	gomega.Expect(err).Should(gomega.BeNil())

	bal, err := client.FetchBalance(context.Background(), common.HexToAddress(""))
	gomega.Expect(err).Should(gomega.BeNil())
	gomega.Expect(bal.Cmp(common.Big0)).Should(gomega.Equal(0))

	err = os.Setenv("RPC_URI", rpcURI)
	gomega.Expect(err).Should(gomega.BeNil())
	cmd := exec.Command("npx", "hardhat", "test", fmt.Sprintf("./test/%s.ts", test), "--network", "local")
	cmd.Dir = "./contract-examples"
	log.Info("Running hardhat command", "cmd", cmd.String())

	out, err := cmd.CombinedOutput()
	fmt.Printf("\nCombined output:\n\n%s\n", string(out))
	if err != nil {
		fmt.Printf("\nErr: %s\n", err.Error())
	}
	gomega.Expect(err).Should(gomega.BeNil())
}

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

func ExecuteHardHatTestOnNewBlockchain(ctx context.Context, test string) {
	log.Info("Executing HardHat tests on a new blockchain", "test", test)

	genesisFilePath := fmt.Sprintf("./tests/precompile/genesis/%s.json", test)

	blockchainID := CreateNewSubnet(ctx, genesisFilePath)
	chainURI := fmt.Sprintf("%s/ext/bc/%s/rpc", DefaultLocalNodeURI, blockchainID)

	log.Info("Created subnet successfully", "ChainURI", chainURI)
	RunHardhatTests(test, chainURI)
}
