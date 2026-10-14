// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

// Codec serializes state sync messages.
var Codec = newCodec()

func newCodec() codec.Manager {
	mgr := codec.NewManager(maxMessageSize)
	c := linearcodec.NewDefault()

	// Skip 2 registrations to maintain type ID compatibility after removing
	// deprecated gossip types.
	c.SkipRegistrations(2)

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(BlockRequest{}),
		c.RegisterType(BlockResponse{}),
		c.RegisterType(LeafsRequest{}),
		c.RegisterType(LeafsResponse{}),
		c.RegisterType(CodeRequest{}),
		c.RegisterType(CodeResponse{}),
	)

	// Skip 3 registrations for deprecated Warp types:
	// MessageSignatureRequest, BlockSignatureRequest, SignatureResponse
	// See https://github.com/ava-labs/coreth/pull/999
	c.SkipRegistrations(3)

	errs.Add(mgr.RegisterCodec(Version, c))

	if errs.Errored() {
		panic(errs.Err)
	}

	return mgr
}
