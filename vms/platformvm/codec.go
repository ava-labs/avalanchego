// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Codecs do serialization and deserialization
var (
	Codec        codec.Manager
	GenesisCodec codec.Manager
)

func init() {
	c := linearcodec.NewDefault()
	Codec = codec.NewDefaultManager()
	gc := linearcodec.NewCustomMaxLength(math.MaxInt32)
	GenesisCodec = codec.NewManager(math.MaxInt32)

	errs := wrappers.Errs{}
	for _, c := range []codec.Registry{c, gc} {
		errs.Add(
			c.RegisterType(&ProposalBlock{}),
			c.RegisterType(&AbortBlock{}),
			c.RegisterType(&CommitBlock{}),
			c.RegisterType(&StandardBlock{}),
			c.RegisterType(&AtomicBlock{}),

			txs.RegisterUnsignedTxsTypes(c),
		)
	}
	errs.Add(
		Codec.RegisterCodec(txs.Version, c),
		GenesisCodec.RegisterCodec(txs.Version, gc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}
