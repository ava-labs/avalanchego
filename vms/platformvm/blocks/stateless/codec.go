// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

const (
	// Version is the current default codec version
	Version = 0
)

// Codec does serialization and deserialization
var Codec codec.Manager

func init() {
	gc := linearcodec.NewCustomMaxLength(math.MaxInt32)
	Codec = codec.NewManager(math.MaxInt32)

	errs := wrappers.Errs{}
	errs.Add(
		RegisterBlockTypes(gc),
		unsigned.RegisterUnsignedTxsTypes(gc),
		Codec.RegisterCodec(Version, gc),
	)

	if errs.Errored() {
		panic(errs.Err)
	}
}

// RegisterBlockTypes allows registering relevant type of blocks package
// in the right sequence. Following repackaging of platformvm package, a few
// subpackage-level codecs were introduced, each handling serialization of specific types.
// RegisterUnsignedTxsTypes is made exportable so to guarantee that other codecs
// are coherent with components one.
func RegisterBlockTypes(targetCodec codec.Registry) error {
	errs := wrappers.Errs{}
	errs.Add(
		targetCodec.RegisterType(&ProposalBlock{}),
		targetCodec.RegisterType(&AbortBlock{}),
		targetCodec.RegisterType(&CommitBlock{}),
		targetCodec.RegisterType(&StandardBlock{}),
		targetCodec.RegisterType(&AtomicBlock{}),
	)
	return errs.Err
}
