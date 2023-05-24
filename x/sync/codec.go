// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	Version        = 0 // TODO danlaine unexport this
	maxMessageSize = 1 * units.MiB
)

var syncCodec codec.Manager

func init() {
	syncCodec = codec.NewManager(maxMessageSize)
	c := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&ChangeProofRequest{}),
		c.RegisterType(&RangeProofRequest{}),
		syncCodec.RegisterCodec(Version, c),
	)

	if errs.Errored() {
		panic(errs.Err)
	}
}
