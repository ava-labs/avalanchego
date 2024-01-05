// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"math"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils"
)

const CodecVersion = 0

var Codec codec.Manager

func init() {
	Codec = codec.NewManager(math.MaxInt)
	lc := linearcodec.NewDefault(time.Time{})

	err := utils.Err(
		lc.RegisterType(&BitSetSignature{}),
		Codec.RegisterCodec(CodecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}
