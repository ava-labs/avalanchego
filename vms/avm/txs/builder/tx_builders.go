// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/x/builder"
	"github.com/ava-labs/avalanchego/wallet/chain/x/signer"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

type TxBuilderBackend interface {
	builder.Backend
	signer.Backend

	Context() *builder.Context
	ResetAddresses(addrs set.Set[ids.ShortID])
}

func BuildCreateAssetTx(
	backend TxBuilderBackend,
	name, symbol string,
	denomination byte,
	initialStates map[uint32][]verify.State,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, ids.ShortID, error) {
	xBuilder, xSigner := builders(backend, kc)

	utx, err := xBuilder.NewCreateAssetTx(
		name,
		symbol,
		denomination,
		initialStates,
		options(changeAddr, nil /*memo*/)...,
	)
	if err != nil {
		return nil, ids.ShortEmpty, fmt.Errorf("failed building base tx: %w", err)
	}

	tx, err := signer.SignUnsigned(context.Background(), xSigner, utx)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	return tx, changeAddr, nil
}

func BuildBaseTx(
	backend TxBuilderBackend,
	outs []*avax.TransferableOutput,
	memo []byte,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, ids.ShortID, error) {
	xBuilder, xSigner := builders(backend, kc)

	utx, err := xBuilder.NewBaseTx(
		outs,
		options(changeAddr, memo)...,
	)
	if err != nil {
		return nil, ids.ShortEmpty, fmt.Errorf("failed building base tx: %w", err)
	}

	tx, err := signer.SignUnsigned(context.Background(), xSigner, utx)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	return tx, changeAddr, nil
}

func MintNFT(
	backend TxBuilderBackend,
	assetID ids.ID,
	payload []byte,
	owners []*secp256k1fx.OutputOwners,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	xBuilder, xSigner := builders(backend, kc)

	utx, err := xBuilder.NewOperationTxMintNFT(
		assetID,
		payload,
		owners,
		options(changeAddr, nil /*memo*/)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed minting NFTs: %w", err)
	}

	return signer.SignUnsigned(context.Background(), xSigner, utx)
}

func MintFTs(
	backend TxBuilderBackend,
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	xBuilder, xSigner := builders(backend, kc)

	utx, err := xBuilder.NewOperationTxMintFT(
		outputs,
		options(changeAddr, nil /*memo*/)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed minting FTs: %w", err)
	}

	return signer.SignUnsigned(context.Background(), xSigner, utx)
}

func BuildOperation(
	backend TxBuilderBackend,
	ops []*txs.Operation,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	xBuilder, xSigner := builders(backend, kc)

	utx, err := xBuilder.NewOperationTx(
		ops,
		options(changeAddr, nil /*memo*/)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building operation tx: %w", err)
	}

	return signer.SignUnsigned(context.Background(), xSigner, utx)
}

func BuildImportTx(
	backend TxBuilderBackend,
	sourceChain ids.ID,
	to ids.ShortID,
	kc *secp256k1fx.Keychain,
) (*txs.Tx, error) {
	xBuilder, xSigner := builders(backend, kc)

	outOwner := &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{to},
	}

	utx, err := xBuilder.NewImportTx(
		sourceChain,
		outOwner,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building import tx: %w", err)
	}

	return signer.SignUnsigned(context.Background(), xSigner, utx)
}

func BuildExportTx(
	backend TxBuilderBackend,
	destinationChain ids.ID,
	to ids.ShortID,
	exportedAssetID ids.ID,
	exportedAmt uint64,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, ids.ShortID, error) {
	xBuilder, xSigner := builders(backend, kc)

	outputs := []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: exportedAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: exportedAmt,
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     []ids.ShortID{to},
			},
		},
	}}

	utx, err := xBuilder.NewExportTx(
		destinationChain,
		outputs,
		options(changeAddr, nil /*memo*/)...,
	)
	if err != nil {
		return nil, ids.ShortEmpty, fmt.Errorf("failed building export tx: %w", err)
	}

	tx, err := signer.SignUnsigned(context.Background(), xSigner, utx)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}
	return tx, changeAddr, nil
}

func builders(backend TxBuilderBackend, kc *secp256k1fx.Keychain) (builder.Builder, signer.Signer) {
	var (
		addrs   = kc.Addresses()
		builder = builder.New(addrs, backend.Context(), backend)
		signer  = signer.New(kc, backend)
	)

	backend.ResetAddresses(addrs)

	return builder, signer
}

func options(changeAddr ids.ShortID, memo []byte) []common.Option {
	return common.UnionOptions(
		[]common.Option{common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		})},
		[]common.Option{common.WithMemo(memo)},
	)
}
