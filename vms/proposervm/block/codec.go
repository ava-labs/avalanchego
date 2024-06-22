// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils"
)

const (
	PreBlockSigCodecVersion = 0
	CurrentCodecVersion     = 1
)

var Codec codec.Manager

func init() {
	// The maximum block size is enforced by the p2p message size limit.
	// See: [constants.DefaultMaxMessageSize]
	Codec = codec.NewManager(math.MaxInt)

	lc0 := linearcodec.NewDefault()
	lc1 := linearcodec.NewDefault()

	err := utils.Err(
		lc0.RegisterType(&preBlockSigStatelessBlock{}),
		lc0.RegisterType(&option{}),
		Codec.RegisterCodec(PreBlockSigCodecVersion, lc0),
		lc1.RegisterType(&statelessBlock{}),
		Codec.RegisterCodec(CurrentCodecVersion, lc1),
	)
	if err != nil {
		panic(err)
	}
}
