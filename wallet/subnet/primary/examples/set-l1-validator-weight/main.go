// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
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
	chainID := ids.FromStringOrPanic("2BMFrJ9xeh5JdwZEx6uuFcjfZC2SV2hdbMT8ee5HrvjtfJb5br")
	address := []byte{}
	validationID := ids.FromStringOrPanic("2Y3ZZZXxpzm46geqVuqFXeSFVbeKihgrfeXRDaiF4ds6R2N8M5")
	nonce := uint64(1)
	weight := uint64(2)
	blsSKHex := "3f783929b295f16cd1172396acb23b20eed057b9afb1caa419e9915f92860b35"

	blsSKBytes, err := hex.DecodeString(blsSKHex)
	if err != nil {
		log.Fatalf("failed to decode secret key: %s\n", err)
	}

	sk, err := localsigner.FromBytes(blsSKBytes)
	if err != nil {
		log.Fatalf("failed to parse secret key: %s\n", err)
	}

	// MakePWallet fetches the available UTXOs owned by [kc] on the P-chain that
	// [uri] is hosting.
	walletSyncStartTime := time.Now()
	ctx := context.Background()
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

	// Get the chain context
	context := wallet.Builder().Context()

	addressedCallPayload, err := message.NewL1ValidatorWeight(
		validationID,
		nonce,
		weight,
	)
	if err != nil {
		log.Fatalf("failed to create L1ValidatorWeight message: %s\n", err)
	}
	addressedCallPayloadJSON, err := json.MarshalIndent(addressedCallPayload, "", "\t")
	if err != nil {
		log.Fatalf("failed to marshal L1ValidatorWeight message: %s\n", err)
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
	signedWarp, err := sk.Sign(unsignedWarp.Bytes())
	if err != nil {
		log.Fatalf("failed to sign Warp message: %s\n", err)
	}

	warp, err := warp.NewMessage(
		unsignedWarp,
		&warp.BitSetSignature{
			Signers: set.NewBits(0).Bytes(),
			Signature: ([bls.SignatureLen]byte)(
				bls.SignatureToBytes(signedWarp),
			),
		},
	)
	if err != nil {
		log.Fatalf("failed to create Warp message: %s\n", err)
	}

	setWeightStartTime := time.Now()
	setWeightTx, err := wallet.IssueSetL1ValidatorWeightTx(
		warp.Bytes(),
	)
	if err != nil {
		log.Fatalf("failed to issue set L1 validator weight transaction: %s\n", err)
	}
	log.Printf("issued set weight of validationID %s to %d with nonce %d and txID %s in %s\n", validationID, weight, nonce, setWeightTx.ID(), time.Since(setWeightStartTime))
}
