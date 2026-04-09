package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

func main() {
	networkDir := flag.String("network-dir", os.Getenv(tmpnet.NetworkDirEnvName), "Path to tmpnet network directory")
	genesisPath := flag.String("genesis", "", "Path to chain genesis JSON file")
	chainsOutput := flag.String("chains-output", "", "Path to write chains.json for the frontend")
	_ = flag.String("config", "", "Path to subnet config JSON file (for future use)")
	flag.Parse()

	if *networkDir == "" {
		log.Fatal("--network-dir or TMPNET_NETWORK_DIR is required")
	}
	if *genesisPath == "" {
		log.Fatal("--genesis is required")
	}

	genesisBytes, err := os.ReadFile(*genesisPath)
	if err != nil {
		log.Fatalf("failed to read genesis file: %s", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	log.Println("reading network...")
	logger := logging.NewLogger("", logging.NewWrappedCore(logging.Info, os.Stderr, logging.Plain.ConsoleEncoder()))
	network, err := tmpnet.ReadNetwork(ctx, logger, *networkDir)
	if err != nil {
		log.Fatalf("failed to read network: %s", err)
	}

	if len(network.Nodes) == 0 {
		log.Fatal("no nodes found in network")
	}

	// Use the first node's URI for API calls
	apiURI := network.Nodes[0].GetAccessibleURI()
	log.Printf("using node API: %s", apiURI)

	// Create wallet with the ewoq pre-funded key
	kc := secp256k1fx.NewKeychain(genesis.EWOQKey)

	log.Println("creating P-chain wallet...")
	pWallet, err := primary.MakePWallet(
		ctx,
		apiURI,
		kc,
		primary.WalletConfig{},
	)
	if err != nil {
		log.Fatalf("failed to create wallet: %s", err)
	}

	// Step 1: Create Subnet
	log.Println("issuing CreateSubnetTx...")
	subnetTx, err := pWallet.IssueCreateSubnetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{genesis.EWOQKey.Address()},
		},
	)
	if err != nil {
		log.Fatalf("failed to create subnet: %s", err)
	}
	subnetID := subnetTx.ID()
	log.Printf("created subnet: %s", subnetID)

	// Re-create wallet with subnet tracked
	pWallet, err = primary.MakePWallet(
		ctx,
		apiURI,
		kc,
		primary.WalletConfig{
			SubnetIDs: []ids.ID{subnetID},
		},
	)
	if err != nil {
		log.Fatalf("failed to recreate wallet with subnet: %s", err)
	}

	// Step 2: Create Chain
	log.Println("issuing CreateChainTx...")
	chainTx, err := pWallet.IssueCreateChainTx(
		subnetID,
		genesisBytes,
		constants.SubnetEVMID,
		nil,
		"simplexl1",
	)
	if err != nil {
		log.Fatalf("failed to create chain: %s", err)
	}
	chainID := chainTx.ID()
	log.Printf("created chain: %s", chainID)

	// Step 3: Collect node BLS keys
	log.Println("collecting node BLS keys...")
	var validators []*txs.ConvertSubnetToL1Validator
	var conversionValidators []message.SubnetToL1ConversionValidatorData

	for _, node := range network.Nodes {
		nodeURI := node.GetAccessibleURI()
		infoClient := info.NewClient(nodeURI)

		nodeID, nodePoP, err := infoClient.GetNodeID(ctx)
		if err != nil {
			log.Fatalf("failed to get node ID from %s: %s", nodeURI, err)
		}

		log.Printf("  node %s: BLS key collected", nodeID)

		validators = append(validators, &txs.ConvertSubnetToL1Validator{
			NodeID:                nodeID.Bytes(),
			Weight:                units.Schmeckle,
			Balance:               units.Avax,
			Signer:                *nodePoP,
			RemainingBalanceOwner: message.PChainOwner{},
			DeactivationOwner:     message.PChainOwner{},
		})

		conversionValidators = append(conversionValidators, message.SubnetToL1ConversionValidatorData{
			NodeID:       nodeID.Bytes(),
			BLSPublicKey: nodePoP.PublicKey,
			Weight:       units.Schmeckle,
		})
	}

	// Step 4: Convert Subnet to L1
	log.Println("issuing ConvertSubnetToL1Tx...")
	address := []byte{}
	convertTx, err := pWallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		validators,
	)
	if err != nil {
		log.Fatalf("failed to convert subnet to L1: %s", err)
	}

	conversionID, err := message.SubnetToL1ConversionID(message.SubnetToL1ConversionData{
		SubnetID:       subnetID,
		ManagerChainID: chainID,
		ManagerAddress: address,
		Validators:     conversionValidators,
	})
	if err != nil {
		log.Fatalf("failed to calculate conversion ID: %s", err)
	}

	fmt.Println("\n=== L1 Created Successfully ===")
	fmt.Printf("Subnet ID:      %s\n", subnetID)
	fmt.Printf("Chain ID:       %s\n", chainID)
	fmt.Printf("Conversion TX:  %s\n", convertTx.ID())
	fmt.Printf("Conversion ID:  %s\n", conversionID)
	fmt.Printf("Validators:     %d nodes\n", len(validators))
	fmt.Printf("VM:             subnet-evm (%s)\n", constants.SubnetEVMID)

	// Print SUBNET_ID= line for shell script to parse
	fmt.Printf("\nSUBNET_ID=%s\n", subnetID)

	// Write chains.json for the frontend
	if *chainsOutput != "" {
		type chainEntry struct {
			Name           string `json:"name"`
			ChainID        string `json:"chainId"`
			RPCPath        string `json:"rpcPath"`
			SubnetID       string `json:"subnetId,omitempty"`
			VM             string `json:"vm"`
			ConversionTx   string `json:"conversionTx,omitempty"`
			ConversionID   string `json:"conversionId,omitempty"`
			ValidatorCount int    `json:"validatorCount,omitempty"`
		}

		// Always start fresh with P-Chain and C-Chain
		chains := []chainEntry{
			{
				Name:    "P-Chain",
				ChainID: "P",
				RPCPath: "/ext/bc/P",
				VM:      "platformvm",
			},
			{
				Name:    "C-Chain",
				ChainID: "C",
				RPCPath: "/ext/bc/C/rpc",
				VM:      "evm",
			},
		}

		// Add the new L1 chain
		chains = append(chains, chainEntry{
			Name:           "simplexl1",
			ChainID:        chainID.String(),
			RPCPath:        fmt.Sprintf("/ext/bc/%s/rpc", chainID),
			SubnetID:       subnetID.String(),
			VM:             "subnet-evm",
			ConversionTx:   convertTx.ID().String(),
			ConversionID:   conversionID.String(),
			ValidatorCount: len(validators),
		})

		if err := os.MkdirAll(filepath.Dir(*chainsOutput), 0o755); err != nil {
			log.Fatalf("failed to create output dir: %s", err)
		}
		data, err := json.MarshalIndent(chains, "", "  ")
		if err != nil {
			log.Fatalf("failed to marshal chains: %s", err)
		}
		if err := os.WriteFile(*chainsOutput, data, 0o644); err != nil {
			log.Fatalf("failed to write chains.json: %s", err)
		}
		fmt.Printf("\nWrote chains to %s\n", *chainsOutput)
	}
}
