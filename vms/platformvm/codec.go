// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"math"

	"github.com/chain4travel/caminogo/codec"
	"github.com/chain4travel/caminogo/codec/linearcodec"
	"github.com/chain4travel/caminogo/utils/wrappers"
	"github.com/chain4travel/caminogo/vms/secp256k1fx"
)

const (
	// CodecVersion is the current default codec version
	CodecVersion = 0
)

// Codecs do serialization and deserialization
var (
	Codec        codec.Manager
	GenesisCodec codec.Manager
)

func init() {
	c := linearcodec.NewDefault()
	Codec = codec.NewDefaultManager()
	gc := linearcodec.NewCustomMaxLength(math.MaxInt32)
	GenesisCodec = codec.NewManager(math.MaxInt32)

	errs := wrappers.Errs{}
	for _, c := range []codec.Registry{c, gc} {
		errs.Add(
			c.RegisterType(&ProposalBlock{}),
			c.RegisterType(&AbortBlock{}),
			c.RegisterType(&CommitBlock{}),
			c.RegisterType(&StandardBlock{}),
			c.RegisterType(&AtomicBlock{}),

			// The Fx is registered here because this is the same place it is
			// registered in the AVM. This ensures that the typeIDs match up for
			// utxos in shared memory.
			c.RegisterType(&secp256k1fx.TransferInput{}),
			c.RegisterType(&secp256k1fx.MintOutput{}),
			c.RegisterType(&secp256k1fx.TransferOutput{}),
			c.RegisterType(&secp256k1fx.MintOperation{}),
			c.RegisterType(&secp256k1fx.Credential{}),
			c.RegisterType(&secp256k1fx.Input{}),
			c.RegisterType(&secp256k1fx.OutputOwners{}),

			c.RegisterType(&UnsignedAddValidatorTx{}),
			c.RegisterType(&UnsignedAddSubnetValidatorTx{}),
			c.RegisterType(&UnsignedAddDelegatorTx{}),

			c.RegisterType(&UnsignedCreateChainTx{}),
			c.RegisterType(&UnsignedCreateSubnetTx{}),

			c.RegisterType(&UnsignedImportTx{}),
			c.RegisterType(&UnsignedExportTx{}),

			c.RegisterType(&UnsignedAdvanceTimeTx{}),
			c.RegisterType(&UnsignedRewardValidatorTx{}),

			c.RegisterType(&StakeableLockIn{}),
			c.RegisterType(&StakeableLockOut{}),
		)
	}
	errs.Add(
		Codec.RegisterCodec(CodecVersion, c),
		GenesisCodec.RegisterCodec(CodecVersion, gc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}
