// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const codecVersion = 0

// The maximum block size is enforced by the p2p message size limit.
// See: [constants.DefaultMaxMessageSize]
//
// Invariant: This codec must never be used to unmarshal a slice unless it is a
// `[]byte`. Otherwise a malicious payload could cause an OOM.
var c codec.Manager

func init() {
	linearCodec := linearcodec.NewCustomMaxLength(math.MaxUint32)
	c = codec.NewManager(math.MaxInt)

	errs := wrappers.Errs{}
	errs.Add(
		linearCodec.RegisterType(&statelessBlock{}),
		linearCodec.RegisterType(&option{}),
		c.RegisterCodec(codecVersion, linearCodec),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}
