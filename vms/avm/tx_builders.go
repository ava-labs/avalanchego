// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/state"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/fees"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/x/signer"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
	walletbuilder "github.com/ava-labs/avalanchego/wallet/chain/x/builder"
)

type txBuilderBackend interface {
	walletbuilder.Backend
	signer.Backend

	State() state.State
	Config() *config.Config
	Codec() codec.Manager

	Context() *walletbuilder.Context
	ResetAddresses(addrs set.Set[ids.ShortID])
}

func buildCreateAssetTx(
	backend txBuilderBackend,
	name, symbol string,
	denomination byte,
	initialStates map[uint32][]verify.State,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, ids.ShortID, error) {
	var (
		pBuilder, pSigner = builders(backend, kc)
		feeCalc           = feeCalculator(backend)
	)

	utx, err := pBuilder.NewCreateAssetTx(
		name,
		symbol,
		denomination,
		initialStates,
		feeCalc,
		options(changeAddr, nil /*memo*/)...,
	)
	if err != nil {
		return nil, ids.ShortEmpty, fmt.Errorf("failed building base tx: %w", err)
	}

	tx, err := signer.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	return tx, changeAddr, nil
}

func buildBaseTx(
	backend txBuilderBackend,
	outs []*avax.TransferableOutput,
	memo []byte,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, ids.ShortID, error) {
	var (
		pBuilder, pSigner = builders(backend, kc)
		feeCalc           = feeCalculator(backend)
	)

	utx, err := pBuilder.NewBaseTx(
		outs,
		feeCalc,
		options(changeAddr, memo)...,
	)
	if err != nil {
		return nil, ids.ShortEmpty, fmt.Errorf("failed building base tx: %w", err)
	}

	tx, err := signer.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}

	return tx, changeAddr, nil
}

func mintNFT(
	backend txBuilderBackend,
	assetID ids.ID,
	payload []byte,
	owners []*secp256k1fx.OutputOwners,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	var (
		pBuilder, pSigner = builders(backend, kc)
		feeCalc           = feeCalculator(backend)
	)

	utx, err := pBuilder.NewOperationTxMintNFT(
		assetID,
		payload,
		owners,
		feeCalc,
		options(changeAddr, nil /*memo*/)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed minting NFTs: %w", err)
	}

	return signer.SignUnsigned(context.Background(), pSigner, utx)
}

func mintFTs(
	backend txBuilderBackend,
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	var (
		pBuilder, pSigner = builders(backend, kc)
		feeCalc           = feeCalculator(backend)
	)
	utx, err := pBuilder.NewOperationTxMintFT(
		outputs,
		feeCalc,
		options(changeAddr, nil /*memo*/)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed minting FTs: %w", err)
	}

	return signer.SignUnsigned(context.Background(), pSigner, utx)
}

func buildOperation(
	backend txBuilderBackend,
	ops []*txs.Operation,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	var (
		pBuilder, pSigner = builders(backend, kc)
		feeCalc           = feeCalculator(backend)
	)

	utx, err := pBuilder.NewOperationTx(
		ops,
		feeCalc,
		options(changeAddr, nil /*memo*/)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building operation tx: %w", err)
	}

	return signer.SignUnsigned(context.Background(), pSigner, utx)
}

func buildImportTx(
	backend txBuilderBackend,
	sourceChain ids.ID,
	to ids.ShortID,
	kc *secp256k1fx.Keychain,
) (*txs.Tx, error) {
	var (
		pBuilder, pSigner = builders(backend, kc)
		feeCalc           = feeCalculator(backend)
	)

	outOwner := &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{to},
	}

	utx, err := pBuilder.NewImportTx(
		sourceChain,
		outOwner,
		feeCalc,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building import tx: %w", err)
	}

	return signer.SignUnsigned(context.Background(), pSigner, utx)
}

func buildExportTx(
	backend txBuilderBackend,
	destinationChain ids.ID,
	to ids.ShortID,
	exportedAssetID ids.ID,
	exportedAmt uint64,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, ids.ShortID, error) {
	var (
		pBuilder, pSigner = builders(backend, kc)
		feeCalc           = feeCalculator(backend)
	)

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

	utx, err := pBuilder.NewExportTx(
		destinationChain,
		outputs,
		feeCalc,
		options(changeAddr, nil /*memo*/)...,
	)
	if err != nil {
		return nil, ids.ShortEmpty, fmt.Errorf("failed building export tx: %w", err)
	}

	tx, err := signer.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}
	return tx, changeAddr, nil
}

func builders(backend txBuilderBackend, kc *secp256k1fx.Keychain) (walletbuilder.Builder, signer.Signer) {
	var (
		addrs   = kc.Addresses()
		builder = walletbuilder.New(addrs, backend.Context(), backend)
		signer  = signer.New(kc, backend)
	)

	backend.ResetAddresses(addrs)

	return builder, signer
}

func feeCalculator(backend txBuilderBackend) *fees.Calculator {
	var (
		chainTime = backend.State().GetTimestamp()
		cfg       = backend.Config()
		isEActive = cfg.IsEActivated(chainTime)
	)

	var feeCalculator *fees.Calculator
	if !isEActive {
		feeCalculator = fees.NewStaticCalculator(cfg)
	} else {
		feeCfg := config.GetDynamicFeesConfig(isEActive)
		feeMan := commonfees.NewManager(feeCfg.FeeRate)
		feeCalculator = fees.NewDynamicCalculator(backend.Codec(), feeMan, feeCfg.BlockMaxComplexity, nil)
	}

	return feeCalculator
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
