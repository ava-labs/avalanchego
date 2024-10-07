// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
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
	chainID := ids.FromStringOrPanic("4R1dLAnG45P3rbdJB2dWuKdVRZF3dLMKgfJ8J6wKSQvYFVUhb")
	addressHex := ""
	validationID := ids.FromStringOrPanic("9FAftNgNBrzHUMMApsSyV6RcFiL9UmCbvsCu28xdLV2mQ7CMo")
	nonce := uint64(1)
	weight := uint64(0)

	address, err := hex.DecodeString(addressHex)
	if err != nil {
		log.Fatalf("failed to decode address %q: %s\n", addressHex, err)
	}

	skBytes, err := os.ReadFile("/Users/stephen/.avalanchego/staking/signer.key")
	if err != nil {
		log.Fatalf("failed to read signer key: %s\n", err)
	}

	sk, err := bls.SecretKeyFromBytes(skBytes)
	if err != nil {
		log.Fatalf("failed to parse secret key: %s\n", err)
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

	addressedCallPayload, err := message.NewSubnetValidatorWeight(
		validationID,
		nonce,
		weight,
	)
	if err != nil {
		log.Fatalf("failed to create SubnetValidatorWeight message: %s\n", err)
	}
	addressedCallPayloadJSON, err := json.MarshalIndent(addressedCallPayload, "", "\t")
	if err != nil {
		log.Fatalf("failed to marshal SubnetValidatorWeight message: %s\n", err)
	}
	log.Println(string(addressedCallPayloadJSON))

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

	signers := set.NewBits()
	signers.Add(0) // [signers] has weight from [vdr[0]]

	unsignedBytes := unsignedWarp.Bytes()
	sig := bls.Sign(sk, unsignedBytes)
	sigBytes := [bls.SignatureLen]byte{}
	copy(sigBytes[:], bls.SignatureToBytes(sig))

	warp, err := warp.NewMessage(
		unsignedWarp,
		&warp.BitSetSignature{
			Signers:   signers.Bytes(),
			Signature: sigBytes,
		},
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
