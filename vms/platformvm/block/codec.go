// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
			RegisterApricotBlockTypes(c),
			txs.RegisterUnsignedTxsTypes(c),
			RegisterBanffBlockTypes(c),
			txs.RegisterDurangoUnsignedTxsTypes(c),
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

// RegisterApricotBlockTypes allows registering relevant type of blocks package
// in the right sequence. Following repackaging of platformvm package, a few
// subpackage-level codecs were introduced, each handling serialization of
// specific types.
func RegisterApricotBlockTypes(targetCodec codec.Registry) error {
	return errors.Join(
		targetCodec.RegisterType(&ApricotProposalBlock{}),
		targetCodec.RegisterType(&ApricotAbortBlock{}),
		targetCodec.RegisterType(&ApricotCommitBlock{}),
		targetCodec.RegisterType(&ApricotStandardBlock{}),
		targetCodec.RegisterType(&ApricotAtomicBlock{}),
	)
}

func RegisterBanffBlockTypes(targetCodec codec.Registry) error {
	return errors.Join(
		targetCodec.RegisterType(&BanffProposalBlock{}),
		targetCodec.RegisterType(&BanffAbortBlock{}),
		targetCodec.RegisterType(&BanffCommitBlock{}),
		targetCodec.RegisterType(&BanffStandardBlock{}),
	)
}
