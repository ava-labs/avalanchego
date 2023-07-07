// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
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
		log.Fatalf("failed to initialize wallet with: %s\n", err)
		return
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

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
	createAssetTx, err := xWallet.IssueCreateAssetTx(
		"RnM",
		"RNM",
		9,
		map[uint32][]verify.State{
			0: {
				&secp256k1fx.TransferOutput{
					Amt:          100 * units.MegaAvax,
					OutputOwners: *owner,
				},
			},
		},
	)
	if err != nil {
		log.Fatalf("failed to create new X-chain asset with: %s\n", err)
		return
	}
	log.Printf("created X-chain asset %s in %s\n", createAssetTx.ID(), time.Since(createAssetStartTime))

	// Send 100 MegaAvax to the P-chain.
	exportStartTime := time.Now()
	exportTx, err := xWallet.IssueExportTx(
		constants.PlatformChainID,
		[]*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: createAssetTx.ID(),
				},
				Out: &secp256k1fx.TransferOutput{
					Amt:          100 * units.MegaAvax,
					OutputOwners: *owner,
				},
			},
		},
	)
	if err != nil {
		log.Fatalf("failed to issue X->P export transaction with: %s\n", err)
		return
	}
	log.Printf("issued X->P export %s in %s\n", exportTx.ID(), time.Since(exportStartTime))

	// Import the 100 MegaAvax from the X-chain into the P-chain.
	importStartTime := time.Now()
	importTx, err := pWallet.IssueImportTx(xChainID, owner)
	if err != nil {
		log.Fatalf("failed to issue X->P import transaction with: %s\n", err)
		return
	}
	log.Printf("issued X->P import %s in %s\n", importTx.ID(), time.Since(importStartTime))

	createSubnetStartTime := time.Now()
	createSubnetTx, err := pWallet.IssueCreateSubnetTx(owner)
	if err != nil {
		log.Fatalf("failed to issue create subnet transaction with: %s\n", err)
		return
	}
	log.Printf("issued create subnet transaction %s in %s\n", createSubnetTx.ID(), time.Since(createSubnetStartTime))

	transformSubnetStartTime := time.Now()
	transformSubnetTx, err := pWallet.IssueTransformSubnetTx(
		createSubnetTx.ID(),
		createAssetTx.ID(),
		50*units.MegaAvax,
		100*units.MegaAvax,
		reward.PercentDenominator,
		reward.PercentDenominator,
		1,
		100*units.MegaAvax,
		time.Second,
		365*24*time.Hour,
		0,
		1,
		5,
		.80*reward.PercentDenominator,
	)
	if err != nil {
		log.Fatalf("failed to issue transform subnet transaction with: %s\n", err)
		return
	}
	log.Printf("issued transform subnet transaction %s in %s\n", transformSubnetTx.ID(), time.Since(transformSubnetStartTime))

	addPermissionlessValidatorStartTime := time.Now()
	startTime := time.Now().Add(time.Minute)
	addSubnetValidatorTx, err := pWallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: genesis.LocalConfig.InitialStakers[0].NodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(startTime.Add(5 * time.Second).Unix()),
				Wght:   25 * units.MegaAvax,
			},
			Subnet: createSubnetTx.ID(),
		},
		&signer.Empty{},
		createAssetTx.ID(),
		&secp256k1fx.OutputOwners{},
		&secp256k1fx.OutputOwners{},
		reward.PercentDenominator,
	)
	if err != nil {
		log.Fatalf("failed to issue add subnet validator with: %s\n", err)
		return
	}
	log.Printf("issued add subnet validator transaction %s in %s\n", addSubnetValidatorTx.ID(), time.Since(addPermissionlessValidatorStartTime))

	addPermissionlessDelegatorStartTime := time.Now()
	addSubnetDelegatorTx, err := pWallet.IssueAddPermissionlessDelegatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: genesis.LocalConfig.InitialStakers[0].NodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(startTime.Add(5 * time.Second).Unix()),
				Wght:   25 * units.MegaAvax,
			},
			Subnet: createSubnetTx.ID(),
		},
		createAssetTx.ID(),
		&secp256k1fx.OutputOwners{},
	)
	if err != nil {
		log.Fatalf("failed to issue add subnet delegator with: %s\n", err)
		return
	}
	log.Printf("issued add subnet validator delegator %s in %s\n", addSubnetDelegatorTx.ID(), time.Since(addPermissionlessDelegatorStartTime))
}
