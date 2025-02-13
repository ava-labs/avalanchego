// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
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
	validationID := ids.FromStringOrPanic("9FAftNgNBrzHUMMApsSyV6RcFiL9UmCbvsCu28xdLV2mQ7CMo")
	balance := uint64(2)

	ctx := context.Background()

	// MakePWallet fetches the available UTXOs owned by [kc] on the P-chain that
	// [uri] is hosting.
	walletSyncStartTime := time.Now()
	wallet, err := primary.MakePWallet(
		ctx,
		uri,
		kc,
		primary.WalletConfig{},
	)
	if err != nil {
		log.Fatalf("failed to initialize wallet: %s\n", err)
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	increaseL1ValidatorBalanceStartTime := time.Now()
	increaseL1ValidatorBalanceTx, err := wallet.IssueIncreaseL1ValidatorBalanceTx(
		validationID,
		balance,
	)
	if err != nil {
		log.Fatalf("failed to issue increase balance transaction: %s\n", err)
	}
	log.Printf("increased balance of validationID %s by %d with %s in %s\n",
		validationID,
		balance,
		increaseL1ValidatorBalanceTx.ID(),
		time.Since(increaseL1ValidatorBalanceStartTime),
	)
}
