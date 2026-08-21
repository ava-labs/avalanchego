// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
)

const (
	// CodecVersion is the version used for full block records, which embed the
	// inner block bytes.
	CodecVersion = 0
	// CodecVersionDedup is the version used for deduplicated block records,
	// which store the block without its inner bytes plus the inner block ID.
	CodecVersionDedup = 1
)

var Codec codec.Manager

func init() {
	lc := linearcodec.NewDefault()
	Codec = codec.NewManager(math.MaxInt32)

	err := errors.Join(
		Codec.RegisterCodec(CodecVersion, lc),
		Codec.RegisterCodec(CodecVersionDedup, lc),
	)
	if err != nil {
		panic(err)
	}
}
