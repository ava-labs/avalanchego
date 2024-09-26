// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/hex"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

func main() {
	key := genesis.EWOQKey
	uri := primary.LocalAPIURI
	kc := secp256k1fx.NewKeychain(key)
	chainID := ids.FromStringOrPanic("2ko3NCPzHeneKWcYfy55pgAgU1LV9Q9XNrNv2sWG4W2XzE3ViV")
	addressHex := ""
	validationID := ids.FromStringOrPanic("225kHLzuaBd6rhxZ8aq91kmgLJyPTFtTFVAWJDaPyKRdDiTpQo")
	nonce := uint64(1)
	weight := uint64(0)

	address, err := hex.DecodeString(addressHex)
	if err != nil {
		log.Fatalf("failed to decode address %q: %s\n", addressHex, err)
	}

	// MakeWallet fetches the available UTXOs owned by [kc] on the network that
	// [uri] is hosting and registers [subnetID].
	walletSyncStartTime := time.Now()
	ctx := context.Background()
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
	context := pWallet.Builder().Context()

	addressedCallPayload, err := message.NewSetSubnetValidatorWeight(
		validationID,
		nonce,
		weight,
	)
	if err != nil {
		log.Fatalf("failed to create SetSubnetValidatorWeight message: %s\n", err)
	}

	addressedCall, err := payload.NewAddressedCall(
		address,
		addressedCallPayload.Bytes(),
	)
	if err != nil {
		log.Fatalf("failed to create AddressedCall message: %s\n", err)
	}

	unsignedWarp, err := warp.NewUnsignedMessage(
		context.NetworkID,
		chainID,
		addressedCall.Bytes(),
	)
	if err != nil {
		log.Fatalf("failed to create unsigned Warp message: %s\n", err)
	}

	warp, err := warp.NewMessage(
		unsignedWarp,
		&warp.BitSetSignature{},
	)
	if err != nil {
		log.Fatalf("failed to create Warp message: %s\n", err)
	}

	setWeightStartTime := time.Now()
	setWeightTx, err := pWallet.IssueSetSubnetValidatorWeightTx(
		warp.Bytes(),
	)
	if err != nil {
		log.Fatalf("failed to issue set subnet validator weight transaction: %s\n", err)
	}
	log.Printf("issued set weight of validationID %s to %d with nonce %d and txID %s in %s\n", validationID, weight, nonce, setWeightTx.ID(), time.Since(setWeightStartTime))
}
