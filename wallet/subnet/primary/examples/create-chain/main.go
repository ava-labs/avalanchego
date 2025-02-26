// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

func main() {
	key := genesis.EWOQKey
	uri := primary.LocalAPIURI
	kc := secp256k1fx.NewKeychain(key)
	subnetIDStr := "BKBZ6xXTnT86B4L5fp8rvtcmNSpvtNz8En9jG61ywV2uWyeHy"

	vmID := constants.SubnetEVMID
	name := "simplexvm"

	genesisBytes, err := os.ReadFile("/Users/yacov.manevich/.avalanche-cli/subnets/yak/genesis.json")
	if err != nil {
		log.Fatalf("failed to read genesis file: %s\n", err)
	}

	subnetID, err := ids.FromString(subnetIDStr)
	if err != nil {
		log.Fatalf("failed to parse subnet ID: %s\n", err)
	}

	ctx := context.Background()

	// MakePWallet fetches the available UTXOs owned by [kc] on the P-chain that
	// [uri] is hosting and registers [subnetID].
	walletSyncStartTime := time.Now()
	wallet, err := primary.MakePWallet(
		ctx,
		uri,
		kc,
		primary.WalletConfig{
			SubnetIDs: []ids.ID{subnetID},
		},
	)
	if err != nil {
		log.Fatalf("failed to initialize wallet: %s\n", err)
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	createChainStartTime := time.Now()
	createChainTx, err := wallet.IssueCreateChainTx(
		subnetID,
		genesisBytes,
		vmID,
		nil,
		name,
	)
	if err != nil {
		log.Fatalf("failed to issue create chain transaction: %s\n", err)
	}
	log.Printf("created new chain %s in %s\n", createChainTx.ID(), time.Since(createChainStartTime))
}
