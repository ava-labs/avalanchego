// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/hex"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

func main() {
	key := genesis.EWOQKey
	uri := primary.LocalAPIURI
	kc := secp256k1fx.NewKeychain(key)
	subnetIDStr := "29uVeLPJB1eQJkzRemU8g8wZDw5uJRqpab5U2mX9euieVwiEbL"
	genesisHex := "00000000000000000000000000017b5490493f8a2fff444ac8b54e27b3339d7c60dcffffffffffffffff"
	vmID := ids.ID{'x', 's', 'v', 'm'}
	name := "let there"

	subnetID, err := ids.FromString(subnetIDStr)
	if err != nil {
		log.Fatalf("failed to parse subnet ID: %s\n", err)
	}

	genesisBytes, err := hex.DecodeString(genesisHex)
	if err != nil {
		log.Fatalf("failed to parse genesis bytes: %s\n", err)
	}

	ctx := context.Background()

	// NewWalletWithTxs fetches the available UTXOs owned by [kc] on the network
	// that [uri] is hosting and registers [subnetID].
	walletSyncStartTime := time.Now()
	wallet, err := primary.NewWalletWithTxs(ctx, uri, kc, subnetID)
	if err != nil {
		log.Fatalf("failed to initialize wallet: %s\n", err)
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	// Get the P-chain wallet
	pWallet := wallet.P()

	createChainStartTime := time.Now()
	createChainTxID, err := pWallet.IssueCreateChainTx(
		subnetID,
		genesisBytes,
		vmID,
		nil,
		name,
	)
	if err != nil {
		log.Fatalf("failed to issue create chain transaction: %s\n", err)
	}
	log.Printf("created new chain %s in %s\n", createChainTxID, time.Since(createChainStartTime))
}
