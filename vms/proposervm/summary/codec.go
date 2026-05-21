// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"errors"
	"math"

	"github.com/MetalBlockchain/metalgo/codec"
	"github.com/MetalBlockchain/metalgo/codec/linearcodec"
)

const CodecVersion = 0

var (
	Codec codec.Manager

	errWrongCodecVersion = errors.New("wrong codec version")
)

func init() {
	lc := linearcodec.NewDefault()
	Codec = codec.NewManager(math.MaxInt32)
	if err := Codec.RegisterCodec(CodecVersion, lc); err != nil {
		panic(err)
	}
}
