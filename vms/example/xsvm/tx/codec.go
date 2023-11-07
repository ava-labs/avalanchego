// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils"
)

// Version is the current default codec version
const Version = 0

var Codec codec.Manager

func init() {
	c := linearcodec.NewCustomMaxLength(math.MaxInt32)
	Codec = codec.NewManager(math.MaxInt32)

	err := utils.Err(
		c.RegisterType(&Transfer{}),
		c.RegisterType(&Export{}),
		c.RegisterType(&Import{}),
		Codec.RegisterCodec(Version, c),
	)
	if err != nil {
		panic(err)
	}
}
