// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platform

import (
	"errors"
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const CodecVersion = 0

var (
	Codec codec.Manager

	// GenesisCodec allows blocks and txs of larger than usual size to be
	// parsed. While this gives flexibility in accommodating large genesis
	// blocks/txs it must not be used to parse new, unverified blocks/txs which
	// instead must be processed by Codec.
	GenesisCodec codec.Manager
)

func init() {
	c := linearcodec.NewDefault()
	gc := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	for _, c := range []linearcodec.Codec{c, gc} {
		errs.Add(
			registerApricotBlockTypes(c),
			registerApricotTxTypes(c),
			registerBanffTxTypes(c),
			registerBanffBlockTypes(c),
			registerDurangoTxTypes(c),
			registerEtnaTxTypes(c),
			registerHeliconTxTypes(c),
		)
	}

	Codec = codec.NewDefaultManager()
	GenesisCodec = codec.NewManager(math.MaxInt32)
	errs.Add(
		Codec.RegisterCodec(CodecVersion, c),
		GenesisCodec.RegisterCodec(CodecVersion, gc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// registerApricotBlockTypes registers the block types that were valid during
// the Apricot series of upgrades. These occupy the first type IDs, ahead of the
// Apricot transaction types.
func registerApricotBlockTypes(targetCodec linearcodec.Codec) error {
	return errors.Join(
		targetCodec.RegisterType(&ApricotProposalBlock{}),
		targetCodec.RegisterType(&ApricotAbortBlock{}),
		targetCodec.RegisterType(&ApricotCommitBlock{}),
		targetCodec.RegisterType(&ApricotStandardBlock{}),
		targetCodec.RegisterType(&ApricotAtomicBlock{}),
	)
}

// registerApricotTxTypes registers the transaction types that were valid during
// the Apricot series of upgrades.
func registerApricotTxTypes(targetCodec linearcodec.Codec) error {
	errs := wrappers.Errs{}

	// The secp256k1fx is registered here because this is the same place it is
	// registered in the AVM. This ensures that the typeIDs match up for utxos
	// in shared memory.
	errs.Add(targetCodec.RegisterType(&secp256k1fx.TransferInput{}))
	targetCodec.SkipRegistrations(1)
	errs.Add(targetCodec.RegisterType(&secp256k1fx.TransferOutput{}))
	targetCodec.SkipRegistrations(1)
	errs.Add(
		targetCodec.RegisterType(&secp256k1fx.Credential{}),
		targetCodec.RegisterType(&secp256k1fx.Input{}),
		targetCodec.RegisterType(&secp256k1fx.OutputOwners{}),

		targetCodec.RegisterType(&AddValidatorTx{}),
		targetCodec.RegisterType(&AddSubnetValidatorTx{}),
		targetCodec.RegisterType(&AddDelegatorTx{}),
		targetCodec.RegisterType(&CreateChainTx{}),
		targetCodec.RegisterType(&CreateSubnetTx{}),
		targetCodec.RegisterType(&ImportTx{}),
		targetCodec.RegisterType(&ExportTx{}),
		targetCodec.RegisterType(&AdvanceTimeTx{}),
		targetCodec.RegisterType(&RewardValidatorTx{}),

		targetCodec.RegisterType(&stakeable.LockIn{}),
		targetCodec.RegisterType(&stakeable.LockOut{}),
	)
	return errs.Err
}

// registerBanffTxTypes registers the transaction types that were
// valid during the Banff series of upgrades.
func registerBanffTxTypes(targetCodec linearcodec.Codec) error {
	return errors.Join(
		targetCodec.RegisterType(&RemoveSubnetValidatorTx{}),
		targetCodec.RegisterType(&TransformSubnetTx{}),
		targetCodec.RegisterType(&AddPermissionlessValidatorTx{}),
		targetCodec.RegisterType(&AddPermissionlessDelegatorTx{}),

		targetCodec.RegisterType(&signer.Empty{}),
		targetCodec.RegisterType(&signer.ProofOfPossession{}),
	)
}

// registerBanffBlockTypes registers the block types that were valid during the
// Banff series of upgrades. These follow the Banff transaction types.
func registerBanffBlockTypes(targetCodec linearcodec.Codec) error {
	return errors.Join(
		targetCodec.RegisterType(&BanffProposalBlock{}),
		targetCodec.RegisterType(&BanffAbortBlock{}),
		targetCodec.RegisterType(&BanffCommitBlock{}),
		targetCodec.RegisterType(&BanffStandardBlock{}),
	)
}

// registerDurangoTxTypes registers the transaction types that were valid during
// the Durango series of upgrades.
func registerDurangoTxTypes(targetCodec linearcodec.Codec) error {
	return errors.Join(
		targetCodec.RegisterType(&TransferSubnetOwnershipTx{}),
		targetCodec.RegisterType(&BaseTx{}),
	)
}

// registerEtnaTxTypes registers the transaction types that
// were valid during the Etna series of upgrades.
func registerEtnaTxTypes(targetCodec linearcodec.Codec) error {
	return errors.Join(
		targetCodec.RegisterType(&ConvertSubnetToL1Tx{}),
		targetCodec.RegisterType(&RegisterL1ValidatorTx{}),
		targetCodec.RegisterType(&SetL1ValidatorWeightTx{}),
		targetCodec.RegisterType(&IncreaseL1ValidatorBalanceTx{}),
		targetCodec.RegisterType(&DisableL1ValidatorTx{}),
	)
}

// registerHeliconTxTypes registers the transaction types that were valid during
// the Helicon series of upgrades.
func registerHeliconTxTypes(targetCodec linearcodec.Codec) error {
	return errors.Join(
		targetCodec.RegisterType(&AddAutoRenewedValidatorTx{}),
		targetCodec.RegisterType(&SetAutoRenewedValidatorConfigTx{}),
		targetCodec.RegisterType(&RewardAutoRenewedValidatorTx{}),
	)
}
