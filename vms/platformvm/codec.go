// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Codec does serialization and deserialization
var Codec codec.Manager

func init() {
	c := linearcodec.NewDefault()
	Codec = codec.NewDefaultManager()

	errs := wrappers.Errs{}
	errs.Add(
		stateless.RegisterBlockTypes(c),
		txs.RegisterUnsignedTxsTypes(c),
		Codec.RegisterCodec(txs.Version, c),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}
