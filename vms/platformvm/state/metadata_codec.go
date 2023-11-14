// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	v0tag = "v0"
	v0    = uint16(0)

	v1tag = "v1"
	v1    = uint16(1)
)

var metadataCodec codec.Manager

func init() {
	c0 := linearcodec.New([]string{v0tag}, math.MaxInt32)
	c1 := linearcodec.New([]string{v0tag, v1tag}, math.MaxInt32)
	metadataCodec = codec.NewManager(math.MaxInt32)

	errs := wrappers.Errs{}
	errs.Add(
		metadataCodec.RegisterCodec(v0, c0),
		metadataCodec.RegisterCodec(v1, c1),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}
