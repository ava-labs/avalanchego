// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	Version        = uint16(0)
	maxMessageSize = 2*units.MiB - 64*units.KiB // Subtract 64 KiB from p2p network cap to leave room for encoding overhead from AvalancheGo
)

var Codec codec.Manager

func init() {
	Codec = codec.NewManager(maxMessageSize)
	c := linearcodec.NewDefault()

	// Skip registration to keep registeredTypes unchanged after legacy gossip deprecation
	// Gossip types and sync summary type removed from codec
	c.SkipRegistrations(2)

	errs := wrappers.Errs{}
	errs.Add(
		// state sync types
		c.RegisterType(BlockRequest{}),
		c.RegisterType(BlockResponse{}),
		c.RegisterType(LeafsRequest{}),
		c.RegisterType(LeafsResponse{}),
		c.RegisterType(CodeRequest{}),
		c.RegisterType(CodeResponse{}),
	)

	// Deprecated Warp request/responde types are skipped
	// See https://github.com/ava-labs/coreth/pull/999
	c.SkipRegistrations(3)

	errs.Add(Codec.RegisterCodec(Version, c))

	if errs.Errored() {
		panic(errs.Err)
	}
}
