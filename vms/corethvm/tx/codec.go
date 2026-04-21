// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const CodecVersion uint16 = 0

var Codec codec.Manager

// TODO: Unexport [Codec] and [CodecVersion]. Serialization should be an
// implementation detail of this package.
func init() {
	Codec = codec.NewDefaultManager()

	var (
		lc   = linearcodec.NewDefault()
		errs = wrappers.Errs{}
	)
	errs.Add(
		lc.RegisterType(&Import{}),
		lc.RegisterType(&Export{}),
	)
	lc.SkipRegistrations(3)
	errs.Add(
		lc.RegisterType(&secp256k1fx.TransferInput{}),
	)
	lc.SkipRegistrations(1)
	errs.Add(
		lc.RegisterType(&secp256k1fx.TransferOutput{}),
	)
	lc.SkipRegistrations(1)
	errs.Add(
		lc.RegisterType(&secp256k1fx.Credential{}),
		lc.RegisterType(&secp256k1fx.Input{}),
		lc.RegisterType(&secp256k1fx.OutputOwners{}),
		Codec.RegisterCodec(CodecVersion, lc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

func MarshalAtomicRequests(r *atomic.Requests) ([]byte, error) {
	return Codec.Marshal(CodecVersion, r)
}
