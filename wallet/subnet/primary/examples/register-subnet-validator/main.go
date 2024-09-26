// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/hex"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

func main() {
	key := genesis.EWOQKey
	uri := "http://localhost:9710"
	kc := secp256k1fx.NewKeychain(key)
	subnetID := ids.FromStringOrPanic("2DeHa7Qb6sufPkmQcFWG2uCd4pBPv9WB6dkzroiMQhd1NSRtof")
	chainID := ids.FromStringOrPanic("2f23iBApzAwJy1LRrgGZ3pGGK4S6UrakHMejZsQiwKy4rKfnzx")
	addressHex := ""
	weight := units.Schmeckle

	address, err := hex.DecodeString(addressHex)
	if err != nil {
		log.Fatalf("failed to decode address %q: %s\n", addressHex, err)
	}

	ctx := context.Background()
	infoClient := info.NewClient(uri)

	nodeInfoStartTime := time.Now()
	nodeID, nodePoP, err := infoClient.GetNodeID(ctx)
	if err != nil {
		log.Fatalf("failed to fetch node IDs: %s\n", err)
	}
	log.Printf("fetched node ID %s in %s\n", nodeID, time.Since(nodeInfoStartTime))

	// MakeWallet fetches the available UTXOs owned by [kc] on the network that
	// [uri] is hosting and registers [subnetID].
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
	context := pWallet.Builder().Context()

	addressedCallPayload, err := message.NewRegisterSubnetValidator(
		subnetID,
		nodeID,
		weight,
		nodePoP.PublicKey,
		uint64(time.Now().Add(5*time.Minute).Unix()),
	)
	if err != nil {
		log.Fatalf("failed to create RegisterSubnetValidator message: %s\n", err)
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

	convertSubnetStartTime := time.Now()
	addValidatorTx, err := pWallet.IssueRegisterSubnetValidatorTx(
		units.Avax,
		nodePoP.ProofOfPossession,
		&secp256k1fx.OutputOwners{},
		warp.Bytes(),
	)
	if err != nil {
		log.Fatalf("failed to issue add subnet validator transaction: %s\n", err)
	}

	validationID := hashing.ComputeHash256Array(addressedCallPayload.Bytes())
	log.Printf("added new subnet validator %s to %s with %s as %s in %s\n", nodeID, subnetID, addValidatorTx.ID(), validationID, time.Since(convertSubnetStartTime))
}
