// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// create_l1 is a demo CLI tool that issues a CreateL1Tx against a running
// tmpnet network and prints the resulting subnet/chain/validator state.
//
// Usage:
//
//	go run ./scripts/demo/create_l1 [--network-dir <path>] [--node-index <n>]
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
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

func main() {
	networkDir := flag.String("network-dir", filepath.Join(os.Getenv("HOME"), ".tmpnet/networks/latest"), "path to tmpnet network directory")
	nodeIndex := flag.Int("node-index", 0, "index of the node to use as validator and wallet target")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	log.Printf("Loading network from %s", *networkDir)

	// ── 1. Load the network ──────────────────────────────────────────────────
	network := &tmpnet.Network{}
	network.Dir = *networkDir
	if err := network.Read(ctx); err != nil {
		log.Fatalf("failed to read network: %v", err)
	}

	if len(network.PreFundedKeys) == 0 {
		log.Fatal("network has no pre-funded keys")
	}
	if len(network.Nodes) == 0 {
		log.Fatal("network has no nodes")
	}
	if *nodeIndex >= len(network.Nodes) {
		log.Fatalf("--node-index %d out of range (network has %d nodes)", *nodeIndex, len(network.Nodes))
	}

	// ── 2. Pick a node to target and use as genesis validator ────────────────
	targetNode := network.Nodes[*nodeIndex]
	nodeURI := targetNode.GetAccessibleURI()
	log.Printf("Targeting node %s at %s", targetNode.NodeID, nodeURI)

	// ── 3. Get the node's NodeID and BLS proof-of-possession ─────────────────
	infoClient := info.NewClient(nodeURI)
	nodeID, pop, err := infoClient.GetNodeID(ctx)
	if err != nil {
		log.Fatalf("failed to get node ID: %v", err)
	}
	log.Printf("Validator NodeID: %s", nodeID)
	log.Printf("Validator BLS PublicKey: %x", pop.PublicKey)

	// ── 4. Build the wallet from the first pre-funded key ────────────────────
	fundedKey := network.PreFundedKeys[0]
	keychain := secp256k1fx.NewKeychain(fundedKey)

	baseWallet, err := primary.MakeWallet(ctx, nodeURI, keychain, keychain, primary.WalletConfig{})
	if err != nil {
		log.Fatalf("failed to create wallet: %v", err)
	}
	wallet := primary.NewWalletWithOptions(
		baseWallet,
		common.WithIssuanceHandler(func(r common.IssuanceReceipt) {
			log.Printf("[issued]    chain=%s txID=%s (%s)", r.ChainAlias, r.TxID, r.Duration)
		}),
		common.WithConfirmationHandler(func(r common.ConfirmationReceipt) {
			log.Printf("[confirmed] chain=%s txID=%s (total=%s confirm=%s)", r.ChainAlias, r.TxID, r.TotalDuration, r.ConfirmationDuration)
		}),
	)
	pWallet := wallet.P()
	_ = pWallet

	// ── 5. Issue the CreateL1Tx ───────────────────────────────────────────────
	// Use empty genesis data and no validator manager for simplicity.
	// The P-chain tx will be accepted regardless of chain startup.
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  Issuing CreateL1Tx...")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// IDs for a chain with no manager and no fxs.
	// Use the TimestampVM which is built into avalanchego (no plugin needed).
	timestampVMID, _ := ids.FromString("tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH")

	var createL1Tx *txs.Tx
	createL1Tx, err = pWallet.IssueCreateL1Tx(
		timestampVMID,
		nil,
		[]byte{},
		ids.Empty,
		[]byte{},
		[]*txs.CreateL1Validator{
			{
				NodeID:  nodeID.Bytes(),
				Weight:  units.Schmeckle,
				Balance: units.Avax,
				Signer:  *pop,
			},
		},
		common.WithContext(ctx),
	)
	if err != nil {
		log.Fatalf("failed to issue CreateL1Tx: %v", err)
	}

	subnetID := createL1Tx.ID()
	genesisValidationID := subnetID.Append(0)

	// ── 6. Derive the deterministic BlockchainID ──────────────────────────────
	blockchainID := createL1Tx.Unsigned.(*txs.CreateL1Tx).BlockchainID(subnetID)

	// ── 7. Query and display results ──────────────────────────────────────────
	pClient := platformvm.NewClient(nodeURI)

	subnet, err := pClient.GetSubnet(ctx, subnetID)
	if err != nil {
		log.Fatalf("failed to get subnet: %v", err)
	}

	height, err := pClient.GetHeight(ctx)
	if err != nil {
		log.Fatalf("failed to get height: %v", err)
	}

	validators, err := pClient.GetValidatorsAt(ctx, subnetID, platformapi.Height(height))
	if err != nil {
		log.Fatalf("failed to get validators: %v", err)
	}

	l1Validator, _, err := pClient.GetL1Validator(ctx, genesisValidationID)
	if err != nil {
		log.Fatalf("failed to get L1 validator: %v", err)
	}

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  CreateL1Tx Result")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  TxID (= SubnetID):    %s\n", createL1Tx.ID())
	fmt.Printf("  SubnetID:             %s\n", subnetID)
	fmt.Printf("  BlockchainID:         %s\n", blockchainID)
	fmt.Printf("  GenesisValidationID:  %s\n", genesisValidationID)
	fmt.Println()
	fmt.Println("  Subnet State:")
	fmt.Printf("    IsPermissioned:     %v\n", subnet.IsPermissioned)
	fmt.Printf("    ConversionID:       %s\n", subnet.ConversionID)
	fmt.Printf("    ManagerChainID:     %s\n", subnet.ManagerChainID)
	fmt.Println()
	fmt.Printf("  Validators (%d):\n", len(validators))
	for id, v := range validators {
		fmt.Printf("    NodeID: %s  Weight: %d\n", id, v.Weight)
	}
	fmt.Println()
	fmt.Println("  L1 Validator:")
	fmt.Printf("    ValidationID:       %s\n", genesisValidationID)
	fmt.Printf("    Weight:             %d\n", l1Validator.Weight)
	fmt.Printf("    Balance:            %d nAVAX\n", l1Validator.Balance)
	fmt.Println()

	// ── 8. Print a JSON summary for scripting ─────────────────────────────────
	summary := map[string]any{
		"txID":                createL1Tx.ID(),
		"subnetID":            subnetID,
		"blockchainID":        blockchainID,
		"genesisValidationID": genesisValidationID,
		"isPermissioned":      subnet.IsPermissioned,
		"conversionID":        subnet.ConversionID,
		"validatorCount":      len(validators),
	}
	summaryJSON, _ := json.MarshalIndent(summary, "  ", "  ")
	fmt.Println("  JSON Summary:")
	fmt.Printf("  %s\n", summaryJSON)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	_ = secp256k1.PrivateKey{}
}
