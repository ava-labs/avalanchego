// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

func main() {
	key := genesis.EWOQKey
	uri := primary.LocalAPIURI
	kc := secp256k1fx.NewKeychain(key)
	assetIDStr := "tWt78T4XYdCSfqXoyhf9WGgbjf9i4GzqTwB9stje2bd6G5kSC"
	addr := key.Address()

	ctx := context.Background()

	customAssetID, err := ids.FromString(assetIDStr)
	if err != nil {
		log.Fatalf("failed to parse assetID: %s\n", err)
	}

	// MakeWallet fetches the available UTXOs owned by [kc] on the network that
	// [uri] is hosting.
	walletSyncStartTime := time.Now()
	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:          uri,
		AVAXKeychain: kc,
		EthKeychain:  kc,
	})
	if err != nil {
		log.Fatalf("failed to initialize wallet: %s\n", err)
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	xWallet := wallet.X()
	pWallet := wallet.P()

	// Pull out useful constants to use when issuing transactions.
	avaxAssetID := xWallet.AVAXAssetID()
	xChainID := xWallet.BlockchainID()
	owner := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			addr,
		},
	}

	exportStartTime := time.Now()
	exportTx, err := xWallet.IssueExportTx(
		constants.PlatformChainID,
		[]*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: avaxAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt:          units.Avax,
					OutputOwners: owner,
				},
			},
			{
				Asset: avax.Asset{
					ID: customAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt:          360 * units.MegaAvax,
					OutputOwners: owner,
				},
			},
		},
	)
	if err != nil {
		log.Fatalf("failed to issue export transaction: %s\n", err)
	}
	log.Printf("issued export %s in %s\n", exportTx.ID(), time.Since(exportStartTime))

	importStartTime := time.Now()
	importTx, err := pWallet.IssueImportTx(xChainID, &owner)
	if err != nil {
		log.Fatalf("failed to issue import transaction: %s\n", err)
	}
	log.Printf("issued import %s in %s\n", importTx.ID(), time.Since(importStartTime))
}
