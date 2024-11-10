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
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

func main() {
	key := genesis.EWOQKey
	uri := primary.LocalAPIURI
	kc := secp256k1fx.NewKeychain(key)
	subnetID := ids.FromStringOrPanic("2DeHa7Qb6sufPkmQcFWG2uCd4pBPv9WB6dkzroiMQhd1NSRtof")
	chainID := ids.FromStringOrPanic("E8nTR9TtRwfkS7XFjTYUYHENQ91mkPMtDUwwCeu7rNgBBtkqu")
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

	validationID := subnetID.Append(0)
	conversionID, err := message.SubnetConversionID(message.SubnetConversionData{
		SubnetID:       subnetID,
		ManagerChainID: chainID,
		ManagerAddress: address,
		Validators: []message.SubnetConversionValidatorData{
			{
				NodeID:       nodeID.Bytes(),
				BLSPublicKey: nodePoP.PublicKey,
				Weight:       weight,
			},
		},
	})
	if err != nil {
		log.Fatalf("failed to calculate conversionID: %s\n", err)
	}

	// MakeWallet fetches the available UTXOs owned by [kc] on the network that
	// [uri] is hosting and registers [subnetID].
	walletSyncStartTime := time.Now()
	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:          uri,
		AVAXKeychain: kc,
		EthKeychain:  kc,
		SubnetIDs:    []ids.ID{subnetID},
	})
	if err != nil {
		log.Fatalf("failed to initialize wallet: %s\n", err)
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	// Get the P-chain wallet
	pWallet := wallet.P()

	convertSubnetStartTime := time.Now()
	convertSubnetTx, err := pWallet.IssueConvertSubnetTx(
		subnetID,
		chainID,
		address,
		[]*txs.ConvertSubnetValidator{
			{
				NodeID:                nodeID.Bytes(),
				Weight:                weight,
				Balance:               units.Avax,
				Signer:                *nodePoP,
				RemainingBalanceOwner: message.PChainOwner{},
				DeactivationOwner:     message.PChainOwner{},
			},
		},
	)
	if err != nil {
		log.Fatalf("failed to issue subnet conversion transaction: %s\n", err)
	}
	log.Printf("converted subnet %s with transactionID %s, validationID %s, and conversionID %s in %s\n",
		subnetID,
		convertSubnetTx.ID(),
		validationID,
		conversionID,
		time.Since(convertSubnetStartTime),
	)
}
