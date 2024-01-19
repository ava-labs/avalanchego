// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

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
	c := linearcodec.NewDefault(time.Time{})
	Codec = codec.NewManager(math.MaxInt32)

	err := utils.Err(
		c.RegisterType(&Transfer{}),
		c.RegisterType(&Export{}),
		c.RegisterType(&Import{}),
		Codec.RegisterCodec(CodecVersion, c),
	)
	if err != nil {
		panic(err)
	}
}
