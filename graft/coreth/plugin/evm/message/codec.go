// (c) 2019-2022, Ava Labs, Inc. All rights reserved.
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

var (
	Codec           codec.Manager
	CrossChainCodec codec.Manager
)

func init() {
	Codec = codec.NewManager(maxMessageSize)
	c := linearcodec.NewCustomMaxLength(maxMessageSize)

	errs := wrappers.Errs{}
	errs.Add(
		// Gossip types
		c.RegisterType(AtomicTxGossip{}),
		c.RegisterType(EthTxsGossip{}),

		// Types for state sync frontier consensus
		c.RegisterType(SyncSummary{}),

		// state sync types
		c.RegisterType(BlockRequest{}),
		c.RegisterType(BlockResponse{}),
		c.RegisterType(LeafsRequest{}),
		c.RegisterType(LeafsResponse{}),
		c.RegisterType(CodeRequest{}),
		c.RegisterType(CodeResponse{}),

		// Warp request types
		c.RegisterType(MessageSignatureRequest{}),
		c.RegisterType(BlockSignatureRequest{}),
		c.RegisterType(SignatureResponse{}),

		Codec.RegisterCodec(Version, c),
	)

	if errs.Errored() {
		panic(errs.Err)
	}

	CrossChainCodec = codec.NewManager(maxMessageSize)
	ccc := linearcodec.NewCustomMaxLength(maxMessageSize)

	errs = wrappers.Errs{}
	errs.Add(
		// CrossChainRequest Types
		ccc.RegisterType(EthCallRequest{}),
		ccc.RegisterType(EthCallResponse{}),

		CrossChainCodec.RegisterCodec(Version, ccc),
	)

	if errs.Errored() {
		panic(errs.Err)
	}
}
