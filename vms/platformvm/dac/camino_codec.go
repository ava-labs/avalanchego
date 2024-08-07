// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Version is the current default codec version
const Version = 0

var Codec codec.Manager

func init() {
	c := linearcodec.NewCaminoDefault()
	Codec = codec.NewDefaultManager()
	gc := linearcodec.NewCaminoCustomMaxLength(math.MaxInt32)

	errs := wrappers.Errs{}
	for _, c := range []linearcodec.CaminoCodec{c, gc} {
		errs.Add(
			c.RegisterCustomType(&BaseFeeProposalState{}),
			c.RegisterCustomType(&AddMemberProposalState{}),
			c.RegisterCustomType(&ExcludeMemberProposalState{}),
			c.RegisterCustomType(&GeneralProposalState{}),
			c.RegisterCustomType(&FeeDistributionProposalState{}),
		)
	}
	errs.Add(
		Codec.RegisterCodec(Version, c),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}
