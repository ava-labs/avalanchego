// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	codecVersion = uint16(0)
)

var vdrCodec codec.Manager

func init() {
	vdrCodec = codec.NewManager(math.MaxInt32)
	c := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(validatorData{}),

		vdrCodec.RegisterCodec(codecVersion, c),
	)

	if errs.Errored() {
		panic(errs.Err)
	}
}
