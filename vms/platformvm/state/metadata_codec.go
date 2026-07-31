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
	CodecVersion0Tag        = "v0"
	CodecVersion0    uint16 = 0

	CodecVersion1Tag        = "v1"
	CodecVersion1    uint16 = 1

	codecVersion2Tag        = "v2"
	codecVersion2    uint16 = 2
)

var MetadataCodec codec.Manager

func init() {
	c0 := linearcodec.New([]string{CodecVersion0Tag})
	c1 := linearcodec.New([]string{CodecVersion0Tag, CodecVersion1Tag})
	c2 := linearcodec.New([]string{CodecVersion0Tag, CodecVersion1Tag, codecVersion2Tag})
	MetadataCodec = codec.NewManager(math.MaxInt32)

	err := errors.Join(
		MetadataCodec.RegisterCodec(CodecVersion0, c0),
		MetadataCodec.RegisterCodec(CodecVersion1, c1),
		MetadataCodec.RegisterCodec(codecVersion2, c2),
	)
	if err != nil {
		panic(err)
	}
}
