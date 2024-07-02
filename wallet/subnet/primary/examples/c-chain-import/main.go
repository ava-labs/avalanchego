// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"
	"time"

	"github.com/ava-labs/coreth/plugin/evm"

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
	avaxAddr := key.Address()
	ethAddr := evm.PublicKeyToEthAddress(key.PublicKey())

	ctx := context.Background()

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

	// Get the P-chain wallet
	pWallet := wallet.P()
	cWallet := wallet.C()

	// Pull out useful constants to use when issuing transactions.
	cContext := cWallet.Builder().Context()
	cChainID := cContext.BlockchainID
	avaxAssetID := cContext.AVAXAssetID
	owner := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			avaxAddr,
		},
	}

	exportStartTime := time.Now()
	exportTx, err := pWallet.IssueExportTx(cChainID, []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: avaxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:          units.Avax,
			OutputOwners: owner,
		},
	}})
	if err != nil {
		log.Fatalf("failed to issue export transaction: %s\n", err)
	}
	log.Printf("issued export %s in %s\n", exportTx.ID(), time.Since(exportStartTime))

	importStartTime := time.Now()
	importTx, err := cWallet.IssueImportTx(constants.PlatformChainID, ethAddr)
	if err != nil {
		log.Fatalf("failed to issue import transaction: %s\n", err)
	}
	log.Printf("issued import %s to %s in %s\n", importTx.ID(), ethAddr.Hex(), time.Since(importStartTime))
}
