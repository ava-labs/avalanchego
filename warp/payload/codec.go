// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"errors"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var errWrongType = errors.New("wrong payload type")

const (
	codecVersion = 0

	MaxMessageSize = 24 * units.KiB

	// Note: Modifying this variable can have subtle implications on memory
	// usage when parsing malformed payloads.
	MaxSliceLen = 24 * units.KiB
)

// Codec does serialization and deserialization for Warp messages.
var c codec.Manager

func init() {
	c = codec.NewManager(MaxMessageSize)
	lc := linearcodec.NewCustomMaxLength(MaxSliceLen)

	errs := wrappers.Errs{}
	errs.Add(
		lc.RegisterType(&AddressedPayload{}),
		lc.RegisterType(&BlockHashPayload{}),
		c.RegisterCodec(codecVersion, lc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}
