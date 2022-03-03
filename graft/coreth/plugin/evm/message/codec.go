// (c) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const Version = uint16(0)
const maxMessageSize = 1 * units.MiB

func BuildCodec() (codec.Manager, error) {
	codecManager := codec.NewManager(maxMessageSize)
	c := linearcodec.NewDefault()
	errs := wrappers.Errs{}
	errs.Add(
		// Gossip types
		c.RegisterType(AtomicTxGossip{}),
		c.RegisterType(EthTxsGossip{}),

		// Types for state sync frontier consensus
		c.RegisterType(SyncableBlock{}),

		// state sync types
		c.RegisterType(BlockRequest{}),
		c.RegisterType(BlockResponse{}),
		c.RegisterType(LeafsRequest{}),
		c.RegisterType(LeafsResponse{}),
		c.RegisterType(CodeRequest{}),
		c.RegisterType(CodeResponse{}),
		c.RegisterType(SerializedMap{}),

		codecManager.RegisterCodec(Version, c),
	)
	return codecManager, errs.Err
}
