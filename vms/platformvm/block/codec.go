// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

const CodecVersion = txs.CodecVersion

var (
	// GenesisCodec allows blocks of larger than usual size to be parsed.
	// While this gives flexibility in accommodating large genesis blocks
	// it must not be used to parse new, unverified blocks which instead
	// must be processed by Codec
	GenesisCodec codec.Manager

	Codec codec.Manager
)

func init() {
	c := linearcodec.NewDefault()
	gc := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	for _, c := range []linearcodec.Codec{c, gc} {
		errs.Add(
			RegisterApricotTypes(c),
			RegisterBanffTypes(c),
			RegisterDurangoTypes(c),
			RegisterEtnaTypes(c),
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

// RegisterApricotTypes registers the type information for blocks that were
// valid during the Apricot series of upgrades.
func RegisterApricotTypes(targetCodec linearcodec.Codec) error {
	return errors.Join(
		targetCodec.RegisterType(&ApricotProposalBlock{}),
		targetCodec.RegisterType(&ApricotAbortBlock{}),
		targetCodec.RegisterType(&ApricotCommitBlock{}),
		targetCodec.RegisterType(&ApricotStandardBlock{}),
		targetCodec.RegisterType(&ApricotAtomicBlock{}),
		txs.RegisterApricotTypes(targetCodec),
	)
}

// RegisterBanffTypes registers the type information for blocks that were valid
// during the Banff series of upgrades.
func RegisterBanffTypes(targetCodec linearcodec.Codec) error {
	return errors.Join(
		txs.RegisterBanffTypes(targetCodec),
		targetCodec.RegisterType(&BanffProposalBlock{}),
		targetCodec.RegisterType(&BanffAbortBlock{}),
		targetCodec.RegisterType(&BanffCommitBlock{}),
		targetCodec.RegisterType(&BanffStandardBlock{}),
	)
}

// RegisterDurangoTypes registers the type information for blocks that were
// valid during the Durango series of upgrades.
func RegisterDurangoTypes(targetCodec linearcodec.Codec) error {
	return txs.RegisterDurangoTypes(targetCodec)
}

// RegisterEtnaTypes registers the type information for blocks that were valid
// during the Etna series of upgrades.
func RegisterEtnaTypes(targetCodec linearcodec.Codec) error {
	return txs.RegisterEtnaTypes(targetCodec)
}
