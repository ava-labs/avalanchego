package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
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

type l1Result struct {
	SubnetID             ids.ID
	ChainID              ids.ID
	ConvertTxID          ids.ID
	ConversionID         ids.ID
	ChainName            string
	ConsensusType        string
	Validators           []*txs.ConvertSubnetToL1Validator
	ConversionValidators []message.SubnetToL1ConversionValidatorData
	ResolvedConfigBytes  []byte
}

func main() {
	networkDir := flag.String("network-dir", os.Getenv(tmpnet.NetworkDirEnvName), "Path to tmpnet network directory")
	genesisPath := flag.String("genesis", "", "Path to chain genesis JSON file")
	chainsOutput := flag.String("chains-output", "", "Path to write chains.json for the frontend")
	configPath := flag.String("config", "", "Path to subnet config JSON file")
	resolvedConfigOutput := flag.String("resolved-config-output", "", "Path to write the resolved subnet config (with validators populated)")
	chainName := flag.String("name", "simplexl1", "Name for the L1 chain")
	flag.Parse()

	if *networkDir == "" {
		log.Fatal("--network-dir or TMPNET_NETWORK_DIR is required")
	}
	if *genesisPath == "" {
		log.Fatal("--genesis is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Load the running tmpnet network from disk
	logger := logging.NewLogger("", logging.NewWrappedCore(logging.Info, os.Stderr, logging.Plain.ConsoleEncoder()))
	network, err := tmpnet.ReadNetwork(ctx, logger, *networkDir)
	if err != nil {
		log.Fatalf("failed to read network: %s", err)
	}
	if len(network.Nodes) == 0 {
		log.Fatal("no nodes found in network")
	}

	// Read the subnet config and determine consensus type
	consensusType, configBytes := readSubnetConfig(*configPath)

	genesisBytes, err := os.ReadFile(*genesisPath)
	if err != nil {
		log.Fatalf("failed to read genesis file: %s", err)
	}

	// Issue P-Chain transactions to create the subnet, chain, and convert to L1
	result := createL1(ctx, network, genesisBytes, configBytes, consensusType, *chainName)

	// Write the resolved config (with validators injected) to disk
	if *resolvedConfigOutput != "" && result.ResolvedConfigBytes != nil {
		if err := os.WriteFile(*resolvedConfigOutput, result.ResolvedConfigBytes, 0o644); err != nil {
			log.Fatalf("failed to write resolved config: %s", err)
		}
		log.Printf("wrote resolved config to %s", *resolvedConfigOutput)
	}

	printResult(result)

	// Update each node's flags to track the new subnet, then restart
	configureAndRestartNodes(ctx, network, result)

	// Write chain metadata for the frontend
	if *chainsOutput != "" {
		writeChainsJSON(*chainsOutput, result)
	}
}

// readSubnetConfig reads the subnet config file and determines the consensus
// type based on which parameter field is present (simplexParameters vs snowParameters).
func readSubnetConfig(configPath string) (string, []byte) {
	if configPath == "" {
		return "snowman", nil
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("failed to read config file: %s", err)
	}

	var configData map[string]json.RawMessage
	if err := json.Unmarshal(configBytes, &configData); err != nil {
		log.Fatalf("failed to parse config file: %s", err)
	}

	consensusType := "snowman"
	if _, ok := configData["simplexParameters"]; ok {
		consensusType = "simplex"
	} else if _, ok := configData["snowParameters"]; ok {
		consensusType = "snowball"
	} else if _, ok := configData["consensusParameters"]; ok {
		consensusType = "snowball"
	}

	log.Printf("consensus type: %s", consensusType)
	return consensusType, configBytes
}

// createL1 issues three P-Chain transactions:
//  1. CreateSubnetTx — creates a new subnet owned by the ewoq key
//  2. CreateChainTx — creates a subnet-evm chain on that subnet
//  3. ConvertSubnetToL1Tx — converts the subnet to an L1 with all network nodes as validators
//
// For simplex configs, it also injects each node's BLS key into the config as initialValidators.
func createL1(
	ctx context.Context,
	network *tmpnet.Network,
	genesisBytes []byte,
	configBytes []byte,
	consensusType string,
	chainName string,
) l1Result {
	apiURI := network.Nodes[0].GetAccessibleURI()
	log.Printf("using node API: %s", apiURI)

	kc := secp256k1fx.NewKeychain(genesis.EWOQKey)

	// Create a P-Chain wallet using the pre-funded ewoq key
	log.Println("creating P-chain wallet...")
	pWallet, err := primary.MakePWallet(ctx, apiURI, kc, primary.WalletConfig{})
	if err != nil {
		log.Fatalf("failed to create wallet: %s", err)
	}

	// Create a new subnet owned by the ewoq key
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

	// Re-create wallet so it tracks the new subnet's UTXO state
	pWallet, err = primary.MakePWallet(ctx, apiURI, kc, primary.WalletConfig{
		SubnetIDs: []ids.ID{subnetID},
	})
	if err != nil {
		log.Fatalf("failed to recreate wallet with subnet: %s", err)
	}

	// Create a subnet-evm chain on the subnet
	log.Println("issuing CreateChainTx...")
	chainTx, err := pWallet.IssueCreateChainTx(
		subnetID,
		genesisBytes,
		constants.SubnetEVMID,
		nil,
		chainName,
	)
	if err != nil {
		log.Fatalf("failed to create chain: %s", err)
	}
	chainID := chainTx.ID()
	log.Printf("created chain: %s", chainID)

	// Collect each node's BLS key for use as L1 validators
	validators, conversionValidators := collectValidators(ctx, network)

	// For simplex, inject node BLS keys as initialValidators in the config
	resolvedConfigBytes := resolveConfig(configBytes, consensusType, conversionValidators)

	// Convert the subnet to an L1 with all nodes as genesis validators
	log.Println("issuing ConvertSubnetToL1Tx...")
	address := []byte{}
	convertTx, err := pWallet.IssueConvertSubnetToL1Tx(subnetID, chainID, address, validators)
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

	return l1Result{
		SubnetID:             subnetID,
		ChainID:              chainID,
		ConvertTxID:          convertTx.ID(),
		ConversionID:         conversionID,
		ChainName:            chainName,
		ConsensusType:        consensusType,
		Validators:           validators,
		ConversionValidators: conversionValidators,
		ResolvedConfigBytes:  resolvedConfigBytes,
	}
}

// collectValidators queries each node's info API for its NodeID and BLS proof
// of possession, and builds the validator structs needed for ConvertSubnetToL1Tx.
func collectValidators(
	ctx context.Context,
	network *tmpnet.Network,
) ([]*txs.ConvertSubnetToL1Validator, []message.SubnetToL1ConversionValidatorData) {
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

		// All validators get equal weight so they have equal voting power in consensus.
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

	return validators, conversionValidators
}

// resolveConfig takes the raw subnet config bytes and, for simplex configs,
// injects the initialValidators field with each node's NodeID and BLS public key.
// For non-simplex configs, it returns the bytes unchanged.
func resolveConfig(
	configBytes []byte,
	consensusType string,
	conversionValidators []message.SubnetToL1ConversionValidatorData,
) []byte {
	if configBytes == nil {
		return nil
	}

	if consensusType != "simplex" {
		return configBytes
	}

	var configMap map[string]json.RawMessage
	if err := json.Unmarshal(configBytes, &configMap); err != nil {
		log.Fatalf("failed to parse config: %s", err)
	}

	var simplexParams map[string]json.RawMessage
	if err := json.Unmarshal(configMap["simplexParameters"], &simplexParams); err != nil {
		log.Fatalf("failed to parse simplexParameters: %s", err)
	}

	type validatorEntry struct {
		NodeID    ids.NodeID `json:"nodeID"`
		PublicKey []byte     `json:"publicKey"`
	}

	var initialValidators []validatorEntry
	for _, cv := range conversionValidators {
		nodeID, err := ids.ToNodeID(cv.NodeID)
		if err != nil {
			log.Fatalf("failed to convert nodeID: %s", err)
		}
		initialValidators = append(initialValidators, validatorEntry{
			NodeID:    nodeID,
			PublicKey: cv.BLSPublicKey[:],
		})
	}

	ivBytes, err := json.Marshal(initialValidators)
	if err != nil {
		log.Fatalf("failed to marshal initialValidators: %s", err)
	}
	simplexParams["initialValidators"] = ivBytes

	spBytes, err := json.Marshal(simplexParams)
	if err != nil {
		log.Fatalf("failed to marshal simplexParameters: %s", err)
	}
	configMap["simplexParameters"] = spBytes

	resolved, err := json.MarshalIndent(configMap, "", "  ")
	if err != nil {
		log.Fatalf("failed to marshal resolved config: %s", err)
	}
	return resolved
}

// configureAndRestartNodes sets track-subnets and subnet-config-content flags
// on each node, writes the config to disk, and restarts the network.
func configureAndRestartNodes(ctx context.Context, network *tmpnet.Network, result l1Result) {
	log.Println("configuring nodes to track subnet...")

	// Build the base64-encoded subnet config content that nodes need
	subnetConfigContent := ""
	if result.ResolvedConfigBytes != nil {
		configMap := map[string]json.RawMessage{
			result.SubnetID.String(): result.ResolvedConfigBytes,
		}
		marshaledConfigs, err := json.Marshal(configMap)
		if err != nil {
			log.Fatalf("failed to marshal subnet configs: %s", err)
		}
		subnetConfigContent = base64.StdEncoding.EncodeToString(marshaledConfigs)
	}

	// Build the base64-encoded chain config to enable debug logging on the L1
	chainConfigMap := map[string]json.RawMessage{
		result.ChainID.String(): json.RawMessage(`{"log-level":"debug"}`),
	}
	marshaledChainConfigs, err := json.Marshal(chainConfigMap)
	if err != nil {
		log.Fatalf("failed to marshal chain configs: %s", err)
	}
	chainConfigContent := base64.StdEncoding.EncodeToString(marshaledChainConfigs)

	for _, node := range network.Nodes {
		node.Flags[config.TrackSubnetsKey] = result.SubnetID.String()
		if subnetConfigContent != "" {
			node.Flags[config.SubnetConfigContentKey] = subnetConfigContent
		}
		node.Flags[config.ChainConfigContentKey] = chainConfigContent
		node.Flags[config.LogLevelKey] = "debug"
		node.Flags[config.LogDisplayLevelKey] = "debug"
		if err := node.Write(); err != nil {
			log.Fatalf("failed to write config for %s: %s", node.NodeID, err)
		}
		log.Printf("  configured %s", node.NodeID)
	}

	log.Println("restarting network...")
	if err := network.Restart(ctx); err != nil {
		log.Fatalf("failed to restart network: %s", err)
	}
	log.Println("network restarted and healthy")
}

func printResult(result l1Result) {
	fmt.Println("\n=== L1 Created Successfully ===")
	fmt.Printf("Subnet ID:      %s\n", result.SubnetID)
	fmt.Printf("Chain ID:       %s\n", result.ChainID)
	fmt.Printf("Conversion TX:  %s\n", result.ConvertTxID)
	fmt.Printf("Conversion ID:  %s\n", result.ConversionID)
	fmt.Printf("Validators:     %d nodes\n", len(result.Validators))
	fmt.Printf("VM:             subnet-evm (%s)\n", constants.SubnetEVMID)
	fmt.Printf("\nSUBNET_ID=%s\n", result.SubnetID)
}

// writeChainsJSON writes the P-Chain, C-Chain, and L1 chain metadata
// to a JSON file for the frontend to consume.
func writeChainsJSON(outputPath string, result l1Result) {
	type chainEntry struct {
		Name           string `json:"name"`
		ChainID        string `json:"chainId"`
		RPCPath        string `json:"rpcPath"`
		SubnetID       string `json:"subnetId,omitempty"`
		VM             string `json:"vm"`
		Consensus      string `json:"consensus,omitempty"`
		ConversionTx   string `json:"conversionTx,omitempty"`
		ConversionID   string `json:"conversionId,omitempty"`
		ValidatorCount int    `json:"validatorCount,omitempty"`
	}

	chains := []chainEntry{
		{
			Name:      "P-Chain",
			ChainID:   "P",
			RPCPath:   "/ext/bc/P",
			VM:        "platformvm",
			Consensus: "snowman",
		},
		{
			Name:      "C-Chain",
			ChainID:   "C",
			RPCPath:   "/ext/bc/C/rpc",
			VM:        "evm",
			Consensus: "snowman",
		},
		{
			Name:           result.ChainName,
			ChainID:        result.ChainID.String(),
			RPCPath:        fmt.Sprintf("/ext/bc/%s/rpc", result.ChainID),
			SubnetID:       result.SubnetID.String(),
			VM:             "subnet-evm",
			Consensus:      result.ConsensusType,
			ConversionTx:   result.ConvertTxID.String(),
			ConversionID:   result.ConversionID.String(),
			ValidatorCount: len(result.Validators),
		},
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		log.Fatalf("failed to create output dir: %s", err)
	}
	data, err := json.MarshalIndent(chains, "", "  ")
	if err != nil {
		log.Fatalf("failed to marshal chains: %s", err)
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		log.Fatalf("failed to write chains.json: %s", err)
	}
	fmt.Printf("\nWrote chains to %s\n", outputPath)
}

