// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"github.com/chain4travel/caminogo/codec"
	"github.com/chain4travel/caminogo/codec/linearcodec"
	"github.com/chain4travel/caminogo/utils/units"
)

const (
	maxPackerSize  = 1 * units.GiB // max size, in bytes, of something being marshalled by Marshal()
	maxSliceLength = 256 * 1024

	codecVersion = 0
)

var c codec.Manager

func init() {
	lc := linearcodec.NewCustomMaxLength(maxSliceLength)
	c = codec.NewManager(maxPackerSize)
	if err := c.RegisterCodec(codecVersion, lc); err != nil {
		panic(err)
	}
}
