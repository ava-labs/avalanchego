// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func ExampleWallet() {
	ctx := context.Background()
	kc := secp256k1fx.NewKeychain(genesis.EWOQKey)

	// NewWalletFromURI fetches the available UTXOs owned by [kc] on the network
	// that [LocalAPIURI] is hosting.
	walletSyncStartTime := time.Now()
	wallet, err := NewWalletFromURI(ctx, LocalAPIURI, kc)
	if err != nil {
		fmt.Printf("failed to initialize wallet with: %s\n", err)
		return
	}
	fmt.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	// Get the P-chain and the X-chain wallets
	pWallet := wallet.P()
	xWallet := wallet.X()

	// Pull out useful constants to use when issuing transactions.
	xChainID := xWallet.BlockchainID()
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			genesis.EWOQKey.PublicKey().Address(),
		},
	}

	// Create a custom asset to send to the P-chain.
	createAssetStartTime := time.Now()
	createAssetTxID, err := xWallet.IssueCreateAssetTx(
		"RnM",
		"RNM",
		9,
		map[uint32][]verify.State{
			0: {
				&secp256k1fx.TransferOutput{
					Amt:          100 * units.Schmeckle,
					OutputOwners: *owner,
				},
			},
		},
	)
	if err != nil {
		fmt.Printf("failed to create new X-chain asset with: %s\n", err)
		return
	}
	fmt.Printf("created X-chain asset %s in %s\n", createAssetTxID, time.Since(createAssetStartTime))

	// Send 100 schmeckles to the P-chain.
	exportStartTime := time.Now()
	exportTxID, err := xWallet.IssueExportTx(
		constants.PlatformChainID,
		[]*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: createAssetTxID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt:          100 * units.Schmeckle,
					OutputOwners: *owner,
				},
			},
		},
	)
	if err != nil {
		fmt.Printf("failed to issue X->P export transaction with: %s\n", err)
		return
	}
	fmt.Printf("issued X->P export %s in %s\n", exportTxID, time.Since(exportStartTime))

	// Import the 100 schmeckles from the X-chain into the P-chain.
	importStartTime := time.Now()
	importTxID, err := pWallet.IssueImportTx(xChainID, owner)
	if err != nil {
		fmt.Printf("failed to issue X->P import transaction with: %s\n", err)
		return
	}
	fmt.Printf("issued X->P import %s in %s\n", importTxID, time.Since(importStartTime))

	// Send 100 schmeckles back to the X-chain.
	exportStartTime = time.Now()
	exportTxID, err = pWallet.IssueExportTx(
		xChainID,
		[]*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: createAssetTxID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt:          100 * units.Schmeckle,
					OutputOwners: *owner,
				},
			},
		},
	)
	if err != nil {
		fmt.Printf("failed to issue P->X export transaction with: %s\n", err)
		return
	}
	fmt.Printf("issued P->X export %s in %s\n", exportTxID, time.Since(exportStartTime))

	// Import the 100 schmeckles from the P-chain into the X-chain.
	importStartTime = time.Now()
	importTxID, err = xWallet.IssueImportTx(constants.PlatformChainID, owner)
	if err != nil {
		fmt.Printf("failed to issue P->X import transaction with: %s\n", err)
		return
	}
	fmt.Printf("issued P->X import %s in %s\n", importTxID, time.Since(importStartTime))
}
