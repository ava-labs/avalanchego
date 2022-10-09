// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const codecVersion = 0

var (
	// TODO: After Banff is activated, this codec can be deleted.
	//
	// Before the Banff upgrade, enforce a 256 KiB maximum block size.
	apricotCodec codec.Manager

	// After the Banff upgrade, the maximum block size is enforced by the
	// p2p message size limit. See: [constants.DefaultMaxMessageSize]
	//
	// Invariant: This codec must never be used to unmarshal a slice unless it
	//            is a `[]byte`. Otherwise a malicious payload could cause an
	//            OOM.
	banffCodec codec.Manager
)

func init() {
	// codec.defaultMaxSize of 256 KiB
	apricotLinearCodec := linearcodec.NewDefault()
	apricotCodec = codec.NewDefaultManager()

	// maximum allowable size
	banffLinearCodec := linearcodec.NewCustomMaxLength(math.MaxUint32)
	banffCodec = codec.NewManager(math.MaxInt)

	errs := wrappers.Errs{}
	errs.Add(
		apricotLinearCodec.RegisterType(&statelessBlock{}),
		apricotLinearCodec.RegisterType(&option{}),
		apricotCodec.RegisterCodec(codecVersion, apricotLinearCodec),

		banffLinearCodec.RegisterType(&statelessBlock{}),
		banffLinearCodec.RegisterType(&option{}),
		banffCodec.RegisterCodec(codecVersion, banffLinearCodec),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}
