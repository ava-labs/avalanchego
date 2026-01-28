// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
)

const (
	UptimeCodecVersion0Tag        = "v0"
	UptimeCodecVersion0    uint16 = 0
)

var UptimeCodec codec.Manager

func init() {
	c0 := linearcodec.New([]string{UptimeCodecVersion0Tag})
	UptimeCodec = codec.NewManager(math.MaxInt32)

	if err := UptimeCodec.RegisterCodec(UptimeCodecVersion0, c0); err != nil {
		panic(err)
	}
}
