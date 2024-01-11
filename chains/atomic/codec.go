// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"math"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
)

const CodecVersion = 0

// Codec is used to marshal and unmarshal dbElements and chain IDs.
var Codec codec.Manager

func init() {
	lc := linearcodec.NewDefault(time.Time{})
	Codec = codec.NewManager(math.MaxInt)
	if err := Codec.RegisterCodec(CodecVersion, lc); err != nil {
		panic(err)
	}
}
