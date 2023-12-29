// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils"
)

const CodecVersion = 0

// The maximum block size is enforced by the p2p message size limit.
// See: [constants.DefaultMaxMessageSize]
var Codec codec.Manager

func init() {
	lc := linearcodec.NewCustomMaxLength(math.MaxUint32)
	Codec = codec.NewManager(math.MaxInt)

	err := utils.Err(
		lc.RegisterType(&statelessBlock{}),
		lc.RegisterType(&option{}),
		Codec.RegisterCodec(CodecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}
