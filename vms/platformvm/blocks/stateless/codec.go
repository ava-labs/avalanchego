// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/codec/reflectcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

const (
	// PreForkVersion is the current default codec version
	PreForkVersion  = 0
	PostForkVersion = 1

	postForkTag = "postFork"
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
	postForkTags := []string{reflectcodec.DefaultTagName, postForkTag}

	preCdc := linearcodec.NewDefault()
	postCdc := linearcodec.NewWithTags(postForkTags)
	Codec = codec.NewDefaultManager()

	preGc := linearcodec.NewCustomMaxLength(math.MaxInt32)
	postGc := linearcodec.New(postForkTags, math.MaxInt32)
	GenesisCodec = codec.NewManager(math.MaxInt32)

	errs := wrappers.Errs{}
	for _, c := range []codec.Registry{preCdc, postCdc, preGc, postGc} {
		errs.Add(
			RegisterPreForkBlockTypes(c),
			txs.RegisterUnsignedTxsTypes(c),
		)
	}
	for _, c := range []codec.Registry{postCdc, postGc} {
		errs.Add(RegisterPostForkBlockTypes(c))
	}

	errs.Add(
		Codec.RegisterCodec(PreForkVersion, preCdc),
		Codec.RegisterCodec(PostForkVersion, postCdc),
		GenesisCodec.RegisterCodec(PreForkVersion, preGc),
		GenesisCodec.RegisterCodec(PostForkVersion, postGc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// RegisterPreForkBlockTypes allows registering relevant type of blocks package
// in the right sequence. Following repackaging of platformvm package, a few
// subpackage-level codecs were introduced, each handling serialization of specific types.
// RegisterUnsignedTxsTypes is made exportable so to guarantee that other codecs
// are coherent with components one.
func RegisterPreForkBlockTypes(targetCodec codec.Registry) error {
	errs := wrappers.Errs{}
	errs.Add(
		targetCodec.RegisterType(&ProposalBlock{}),
		targetCodec.RegisterType(&AbortBlock{}),
		targetCodec.RegisterType(&CommitBlock{}),
		targetCodec.RegisterType(&StandardBlock{}),
		targetCodec.RegisterType(&AtomicBlock{}),
	)
	return errs.Err
}

func RegisterPostForkBlockTypes(targetCodec codec.Registry) error {
	errs := wrappers.Errs{}
	errs.Add(
		targetCodec.RegisterType(&PostForkProposalBlock{}),
		targetCodec.RegisterType(&PostForkStandardBlock{}),
	)
	return errs.Err
}
