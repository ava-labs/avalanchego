// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
)

const (
	CodecVersion0Tag = "v0"
	CodecVersionV0   = 0

	CodecVersion1Tag = "v1"
	CodecVersion     = 1
)

var Codec codec.Manager

func init() {
	lcV0 := linearcodec.New([]string{CodecVersion0Tag})
	lc := linearcodec.New([]string{CodecVersion0Tag, CodecVersion1Tag})

	// The maximum block size is enforced by the p2p message size limit.
	// See: [constants.DefaultMaxMessageSize]
	Codec = codec.NewManager(math.MaxInt)

	err := errors.Join(
		lcV0.RegisterType(&statelessBlock{}),
		lcV0.RegisterType(&option{}),
		lc.RegisterType(&statelessBlock{}),
		Codec.RegisterCodec(CodecVersionV0, lcV0),
		Codec.RegisterCodec(CodecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}
