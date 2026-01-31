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

	// warpSkipRegistrations is the number of deprecated Warp request/response types to skip
	// See https://github.com/ava-labs/coreth/pull/999
	warpSkipRegistrations = 3
)

// CodecType identifies the codec variant to use for message serialization.
// Each variant has its own skip count and LeafsRequest wire format.
type CodecType int

const (
	// CorethCodecType is used for C-Chain state sync messages.
	// It skips 3 type registrations (deprecated gossip types and sync summary type)
	// and uses CorethLeafsRequest wire format.
	CorethCodecType CodecType = iota
	// SubnetEVMCodecType is used for Subnet-EVM state sync messages.
	// It skips 2 type registrations (deprecated gossip types)
	// and uses SubnetEVMLeafsRequest wire format.
	SubnetEVMCodecType
)

var (
	// CorethCodec is used for C-Chain state sync messages
	CorethCodec codec.Manager
	// SubnetEVMCodec is used for Subnet-EVM state sync messages
	SubnetEVMCodec codec.Manager
)

func init() {
	CorethCodec = newCodec(CorethCodecType)
	SubnetEVMCodec = newCodec(SubnetEVMCodecType)
}

func newCodec(codecType CodecType) codec.Manager {
	mgr := codec.NewManager(maxMessageSize)
	c := linearcodec.NewDefault()

	errs := wrappers.Errs{}

	// Skip registration to keep registeredTypes unchanged after legacy gossip deprecation
	// Gossip types and sync summary type removed from codec
	switch codecType {
	case SubnetEVMCodecType:
		// Skip 2 registrations for deprecated gossip types
		c.SkipRegistrations(2)
	default:
		// Skip 3 registrations for deprecated gossip types and sync summary type
		c.SkipRegistrations(3)
	}

	errs.Add(
		// state sync types
		c.RegisterType(BlockRequest{}),
		c.RegisterType(BlockResponse{}),
	)

	// Register the concrete leafs request type for the wire format.
	// Must register concrete type, not interface, for codec to serialize correctly.
	switch codecType {
	case SubnetEVMCodecType:
		errs.Add(c.RegisterType(SubnetEVMLeafsRequest{}))
	default:
		errs.Add(c.RegisterType(CorethLeafsRequest{}))
	}

	errs.Add(
		c.RegisterType(LeafsResponse{}),
		c.RegisterType(CodeRequest{}),
		c.RegisterType(CodeResponse{}),
	)

	// Deprecated Warp request/response types are skipped
	c.SkipRegistrations(warpSkipRegistrations)

	errs.Add(mgr.RegisterCodec(Version, c))

	if errs.Errored() {
		panic(errs.Err)
	}

	return mgr
}
