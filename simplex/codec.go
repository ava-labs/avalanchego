// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"math"

	"github.com/MetalBlockchain/metalgo/codec"
	"github.com/MetalBlockchain/metalgo/codec/linearcodec"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/warp"
)

const CodecVersion = warp.CodecVersion + 1

var Codec codec.Manager

func init() {
	lc := linearcodec.NewDefault()

	Codec = codec.NewManager(math.MaxInt)

	if err := Codec.RegisterCodec(CodecVersion, lc); err != nil {
		panic(err)
	}
}
