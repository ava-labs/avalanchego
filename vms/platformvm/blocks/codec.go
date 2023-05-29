// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Version0 is the current default codec version
const (
	Version0 = txs.Version0
	Version1 = txs.Version1
)

// GenesisCode allows blocks of larger than usual size to be parsed.
// While this gives flexibility in accommodating large genesis blocks
// it must not be used to parse new, unverified blocks which instead
// must be processed by Codec
var (
	Codec        codec.Manager
	GenesisCodec codec.Manager
)

func init() {
	c0 := linearcodec.New([]string{"serialize"}, linearcodec.DefaultMaxSliceLength)
	c1 := linearcodec.New([]string{"serialize", txs.TagV1}, linearcodec.DefaultMaxSliceLength)
	Codec = codec.NewDefaultManager()

	gC0 := linearcodec.New([]string{"serialize"}, math.MaxInt32)
	gC1 := linearcodec.New([]string{"serialize", txs.TagV1}, math.MaxInt32)
	GenesisCodec = codec.NewManager(math.MaxInt32)

	errs := wrappers.Errs{}
	for _, c := range []codec.Registry{c0, c1, gC0, gC1} {
		errs.Add(
			RegisterApricotBlockTypes(c),
			txs.RegisterUnsignedTxsTypes(c),
			RegisterBanffBlockTypes(c),
		)
	}
	errs.Add(
		Codec.RegisterCodec(Version1, c1),
		GenesisCodec.RegisterCodec(Version1, gC1),

		Codec.RegisterCodec(Version0, c0),
		GenesisCodec.RegisterCodec(Version0, gC0),
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
	errs := wrappers.Errs{}
	errs.Add(
		targetCodec.RegisterType(&ApricotProposalBlock{}),
		targetCodec.RegisterType(&ApricotAbortBlock{}),
		targetCodec.RegisterType(&ApricotCommitBlock{}),
		targetCodec.RegisterType(&ApricotStandardBlock{}),
		targetCodec.RegisterType(&ApricotAtomicBlock{}),
	)
	return errs.Err
}

func RegisterBanffBlockTypes(targetCodec codec.Registry) error {
	errs := wrappers.Errs{}
	errs.Add(
		targetCodec.RegisterType(&BanffProposalBlock{}),
		targetCodec.RegisterType(&BanffAbortBlock{}),
		targetCodec.RegisterType(&BanffCommitBlock{}),
		targetCodec.RegisterType(&BanffStandardBlock{}),
	)
	return errs.Err
}
