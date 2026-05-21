// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"errors"

	"github.com/MetalBlockchain/metalgo/codec"
	"github.com/MetalBlockchain/metalgo/codec/linearcodec"
	"github.com/MetalBlockchain/metalgo/codec/reflectcodec"
	"github.com/MetalBlockchain/metalgo/utils/units"
)

const (
	CodecVersion            uint16 = 0
	CodecVersionWithStopVtx uint16 = 1

	// maxSize is the maximum allowed vertex size. It is necessary to deter DoS
	maxSize = units.MiB
)

var Codec codec.Manager

func init() {
	lc0 := linearcodec.New([]string{reflectcodec.DefaultTagName + "V0"})
	lc1 := linearcodec.New([]string{reflectcodec.DefaultTagName + "V1"})

	Codec = codec.NewManager(maxSize)
	err := errors.Join(
		Codec.RegisterCodec(CodecVersion, lc0),
		Codec.RegisterCodec(CodecVersionWithStopVtx, lc1),
	)
	if err != nil {
		panic(err)
	}
}
