// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	codecVersion = 0

	MaxMessageSize = 24 * units.KiB

	// Note: Modifying this variable can have subtle implications on memory
	// usage when parsing malformed payloads.
	MaxSliceLen = 24 * 1024
)

// Codec does serialization and deserialization for Warp messages.
var c codec.Manager

func init() {
	c = codec.NewManager(MaxMessageSize)
	lc := linearcodec.NewCustomMaxLength(MaxSliceLen)

	errs := wrappers.Errs{}
	errs.Add(
		lc.RegisterType(&Hash{}),
		lc.RegisterType(&AddressedCall{}),
		c.RegisterCodec(codecVersion, lc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}
