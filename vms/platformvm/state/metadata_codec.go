// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
)

var MetadataCodec codec.Manager

func init() {
	c0 := linearcodec.New([]string{CodecVersion0Tag})
	c1 := linearcodec.New([]string{CodecVersion0Tag, CodecVersion1Tag})
	MetadataCodec = codec.NewManager(math.MaxInt32)

	err := errors.Join(
		MetadataCodec.RegisterCodec(CodecVersion0, c0),
		MetadataCodec.RegisterCodec(CodecVersion1, c1),
	)
	if err != nil {
		panic(err)
	}
}
