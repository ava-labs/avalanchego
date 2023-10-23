// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

func main() {
	key := genesis.EWOQKey
	uri := primary.FujiAPIURI
	kc := secp256k1fx.NewKeychain(key)
	subnetIDStr := "2TkAkG8Tn522nMsKDec3CTHfvaSb4kGrgs2Ho39VvmaLysjayE"
	assetIDStr := "tWt78T4XYdCSfqXoyhf9WGgbjf9i4GzqTwB9stje2bd6G5kSC"

	ctx := context.Background()

	subnetID, err := ids.FromString(subnetIDStr)
	if err != nil {
		log.Fatalf("failed to parse subnet ID: %s\n", err)
	}

	assetID, err := ids.FromString(assetIDStr)
	if err != nil {
		log.Fatalf("failed to parse asset ID: %s\n", err)
	}

	// MakeWallet fetches the available UTXOs owned by [kc] on the network that
	// [uri] is hosting.
	walletSyncStartTime := time.Now()
	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:              uri,
		AVAXKeychain:     kc,
		EthKeychain:      kc,
		PChainTxsToFetch: set.Of(subnetID),
	})
	if err != nil {
		log.Fatalf("failed to initialize wallet: %s\n", err)
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	// Get the P-chain wallet
	pWallet := wallet.P()

	transformSubnetStartTime := time.Now()
	transformSubnetTx, err := pWallet.IssueTransformSubnetTx(
		subnetID,
		assetID,
		360*units.MegaAvax,
		720*units.MegaAvax,
		.10*reward.PercentDenominator,
		.12*reward.PercentDenominator,
		2*units.KiloAvax,
		3*units.MegaAvax,
		2*7*24*time.Hour,
		365*24*time.Hour,
		.02*reward.PercentDenominator,
		25*units.Avax,
		5,
		.8*reward.PercentDenominator,
	)
	if err != nil {
		log.Fatalf("failed to issue transform subnet transaction: %s\n", err)
	}
	log.Printf("issued transform subnet %s in %s\n", transformSubnetTx.ID(), time.Since(transformSubnetStartTime))
}
