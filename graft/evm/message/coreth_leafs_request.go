// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/ids"
)

// CorethLeafsRequest preserves the original coreth wire format where NodeType is serialized.
//
// Note: The fields Root, Account, Start, End, and Limit are duplicated in SubnetEVMLeafsRequest.
// This duplication is intentional because the avalanchego codec requires all serialized fields
// to be exported (start with uppercase). Using an embedded struct would require exporting it,
// which would unnecessarily expose internal types in the public API. By duplicating fields,
// we keep the implementation details private while ensuring correct serialization.
//
// TODO: In a future network upgrade, there will be only one unified wire format for both
// coreth and subnet-evm. At that point, this distinction between CorethLeafsRequest and
// SubnetEVMLeafsRequest will be removed.
type CorethLeafsRequest struct {
	Root     common.Hash `serialize:"true"`
	Account  common.Hash `serialize:"true"`
	Start    []byte      `serialize:"true"`
	End      []byte      `serialize:"true"`
	Limit    uint16      `serialize:"true"`
	NodeType NodeType    `serialize:"true"`
}

func (c CorethLeafsRequest) Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, handler RequestHandler) ([]byte, error) {
	return handler.HandleLeafsRequest(ctx, nodeID, requestID, c)
}

func (c CorethLeafsRequest) String() string {
	return fmt.Sprintf(
		"LeafsRequest(Root=%s, Account=%s, Start=%s, End=%s, Limit=%d, NodeType=%d)",
		c.Root, c.Account, common.Bytes2Hex(c.Start), common.Bytes2Hex(c.End), c.Limit, c.NodeType,
	)
}

func (c CorethLeafsRequest) RootHash() common.Hash    { return c.Root }
func (c CorethLeafsRequest) AccountHash() common.Hash { return c.Account }
func (c CorethLeafsRequest) StartKey() []byte         { return c.Start }
func (c CorethLeafsRequest) EndKey() []byte           { return c.End }
func (c CorethLeafsRequest) KeyLimit() uint16         { return c.Limit }
func (c CorethLeafsRequest) LeafType() NodeType       { return c.NodeType }
