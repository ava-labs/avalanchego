// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"errors"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	CodecVersion = 0

	MaxMessageSize = 24 * units.KiB
)

var Codec codec.Manager

func init() {
	Codec = codec.NewManager(MaxMessageSize)
	lc := linearcodec.NewDefault()

	err := errors.Join(
		lc.RegisterType(&Hash{}),
		lc.RegisterType(&AddressedCall{}),
		Codec.RegisterCodec(CodecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}
