// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils"
)

const (
	CodecVersion0Tag        = "v0"
	CodecVersion0    uint16 = 0

	CodecVersion1Tag        = "v1"
	CodecVersion1    uint16 = 1
)

var MetadataCodec codec.Manager

func init() {
	c0 := linearcodec.New(time.Time{}, []string{CodecVersion0Tag}, math.MaxInt32)
	c1 := linearcodec.New(time.Time{}, []string{CodecVersion0Tag, CodecVersion1Tag}, math.MaxInt32)
	MetadataCodec = codec.NewManager(math.MaxInt32)

	err := utils.Err(
		MetadataCodec.RegisterCodec(CodecVersion0, c0),
		MetadataCodec.RegisterCodec(CodecVersion1, c1),
	)
	if err != nil {
		panic(err)
	}
}
