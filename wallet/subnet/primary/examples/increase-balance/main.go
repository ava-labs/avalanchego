// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

	increaseBalanceStartTime := time.Now()
	increaseBalanceTx, err := pWallet.IssueIncreaseBalanceTx(
		validationID,
		balance,
	)
	if err != nil {
		log.Fatalf("failed to issue increase balance transaction: %s\n", err)
	}
	log.Printf("increased balance of validationID %s by %d with %s in %s\n",
		validationID,
		balance,
		increaseBalanceTx.ID(),
		time.Since(increaseBalanceStartTime),
	)
}
