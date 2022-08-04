// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/codec/reflectcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

const blueberryTag = "blueberry"

// GenesisCode allows blocks of larger than usual size to be parsed.
// While this gives flexibility in accommodating large genesis blocks
// it must not be used to parse new, unverified blocks which instead
// must be processed by Codec
var (
	Codec        codec.Manager
	GenesisCodec codec.Manager
)

func init() {
	blueberryTags := []string{reflectcodec.DefaultTagName, blueberryTag}

	apricotCdc := linearcodec.NewDefault()
	blueberryCdc := linearcodec.NewWithTags(blueberryTags)
	Codec = codec.NewDefaultManager()

	preGc := linearcodec.NewCustomMaxLength(math.MaxInt32)
	postGc := linearcodec.New(blueberryTags, math.MaxInt32)
	GenesisCodec = codec.NewManager(math.MaxInt32)

	errs := wrappers.Errs{}
	for _, c := range []codec.Registry{apricotCdc, blueberryCdc, preGc, postGc} {
		errs.Add(
			RegisterApricotBlockTypes(c),
			txs.RegisterUnsignedTxsTypes(c),
		)
	}
	for _, c := range []codec.Registry{blueberryCdc, postGc} {
		errs.Add(RegisterBlueberryBlockTypes(c))
	}

	errs.Add(
		Codec.RegisterCodec(version.ApricotBlockVersion, apricotCdc),
		Codec.RegisterCodec(version.BlueberryBlockVersion, blueberryCdc),
		GenesisCodec.RegisterCodec(version.ApricotBlockVersion, preGc),
		GenesisCodec.RegisterCodec(version.BlueberryBlockVersion, postGc),
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
		targetCodec.RegisterType(&AtomicBlock{}),
	)
	return errs.Err
}

func RegisterBlueberryBlockTypes(targetCodec codec.Registry) error {
	errs := wrappers.Errs{}
	errs.Add(
		targetCodec.RegisterType(&BlueberryProposalBlock{}),
		targetCodec.RegisterType(&BlueberryAbortBlock{}),
		targetCodec.RegisterType(&BlueberryCommitBlock{}),
		targetCodec.RegisterType(&BlueberryStandardBlock{}),
	)
	return errs.Err
}
