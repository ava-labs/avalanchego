// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	CodecVersion = 0

	MaxMessageSize = 24 * units.KiB

	// Note: Modifying this variable can have subtle implications on memory
	// usage when parsing malformed payloads.
	MaxSliceLen = 24 * 1024
)

var Codec codec.Manager

func init() {
	Codec = codec.NewManager(MaxMessageSize)
	lc := linearcodec.NewCustomMaxLength(MaxSliceLen)

	err := utils.Err(
		lc.RegisterType(&Hash{}),
		lc.RegisterType(&AddressedCall{}),
		Codec.RegisterCodec(CodecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}
