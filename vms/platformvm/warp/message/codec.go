// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
)

const CodecVersion = 0

var Codec codec.Manager

func init() {
	Codec = codec.NewManager(math.MaxInt)
	lc := linearcodec.NewDefault()

	err := errors.Join(
		lc.RegisterType(&SubnetToL1Conversion{}),
		lc.RegisterType(&RegisterL1Validator{}),
		lc.RegisterType(&L1ValidatorRegistration{}),
		lc.RegisterType(&L1ValidatorWeight{}),
		lc.RegisterType(&ValidatorSetState{}),
		Codec.RegisterCodec(CodecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}
