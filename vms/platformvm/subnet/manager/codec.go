// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package manager

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const CodecVersion = 0

var (
	Codec codec.Manager
)

func init() {
	c := linearcodec.NewDefault()
	errs := wrappers.Errs{}

	Codec = codec.NewDefaultManager()
	errs.Add(
		c.RegisterType(&SetSubnetManagerTxWarpMessagePayload{}),
		Codec.RegisterCodec(CodecVersion, c),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}
