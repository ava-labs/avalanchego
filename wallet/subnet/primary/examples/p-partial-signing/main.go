// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	"github.com/ava-labs/avalanchego/wallet/chain/p/signer"
	"github.com/ava-labs/avalanchego/wallet/chain/p/wallet"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

func makeWallet(
	ctx context.Context,
	uri string,
	addresses set.Set[ids.ShortID],
	keychains ...keychain.Keychain,
) (builder.Builder, []signer.Signer, error) {
	state, err := primary.FetchState(ctx, uri, addresses)
	if err != nil {
		return nil, nil, err
	}

	utxos := common.NewChainUTXOs(constants.PlatformChainID, state.UTXOs)
	backend := wallet.NewBackend(state.PCTX, utxos, make(map[ids.ID]fx.Owner))
	builder := builder.New(addresses, state.PCTX, backend)

	signers := make([]signer.Signer, len(keychains))
	for i, keychain := range keychains {
		signers[i] = signer.New(keychain, backend)
	}
	return builder, signers, nil
}

func main() {
	ctx := context.Background()
	builder, signers, err := makeWallet(
		ctx,
		primary.LocalAPIURI,
		set.Of(
			genesis.EWOQKey.Address(),
			genesis.VMRQKey.Address(),
		),
		secp256k1fx.NewKeychain(genesis.EWOQKey),
		secp256k1fx.NewKeychain(genesis.VMRQKey),
	)
	if err != nil {
		log.Fatal(err)
	}

	pContext := builder.Context()
	unsignedTx, err := builder.NewBaseTx([]*avax.TransferableOutput{
		{
			Asset: avax.Asset{ID: pContext.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 27 * units.MegaAvax,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 2,
					Addrs: []ids.ShortID{
						genesis.EWOQKey.Address(),
						genesis.VMRQKey.Address(),
					},
				},
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	signedTx := &txs.Tx{Unsigned: unsignedTx}
	for _, signer := range signers {
		if err := signer.Sign(ctx, signedTx); err != nil {
			log.Fatal(err)
		}
	}

	client := platformvm.NewClient(primary.LocalAPIURI)
	txID, err := client.IssueTx(ctx, signedTx.Bytes())
	if err != nil {
		log.Fatal(err)
	}

	txJSON, err := json.MarshalIndent(signedTx, "", "\t")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Issued transaction %s\n%s", txID, string(txJSON))
}
