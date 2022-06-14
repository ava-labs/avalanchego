// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/codec/reflectcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

const (
	// PreForkVersion is the current default codec version
	PreForkVersion  = 0
	PostForkVersion = 1

	postForkTag = "postFork"
)

// Codec does serialization and deserialization
var Codec codec.Manager

func init() {
	postForkTags := []string{reflectcodec.DefaultTagName, postForkTag}
	preCdc := linearcodec.NewDefault()
	postCdc := linearcodec.NewWithTags(postForkTags)
	Codec = codec.NewDefaultManager()

	preGc := linearcodec.NewCustomMaxLength(math.MaxInt32)
	postGc := linearcodec.New(postForkTags, math.MaxInt32)

	errs := wrappers.Errs{}
	for _, c := range []codec.Registry{preCdc, postCdc, preGc, postGc} {
		errs.Add(
			RegisterBlockTypes(c),
			unsigned.RegisterUnsignedTxsTypes(c),
		)
	}
	errs.Add(
		Codec.RegisterCodec(PreForkVersion, preCdc),
		Codec.RegisterCodec(PostForkVersion, postCdc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// RegisterBlockTypes allows registering relevant type of blocks package
// in the right sequence. Following repackaging of platformvm package, a few
// subpackage-level codecs were introduced, each handling serialization of specific types.
// RegisterUnsignedTxsTypes is made exportable so to guarantee that other codecs
// are coherent with components one.
func RegisterBlockTypes(targetCodec codec.Registry) error {
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
