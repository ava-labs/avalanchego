// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/ids"
)

// SubnetEVMLeafsRequest preserves the original subnet-evm wire format where NodeType is not serialized.
//
// Note: The fields Root, Account, Start, End, and Limit are duplicated in CorethLeafsRequest.
// This duplication is intentional because the avalanchego codec requires all serialized fields
// to be exported (start with uppercase). Using an embedded struct would require exporting it,
// which would unnecessarily expose internal types in the public API. By duplicating fields,
// we keep the implementation details private while ensuring correct serialization.
//
// NOTE: NodeType is not serialized to maintain backward compatibility with subnet-evm nodes.
//
// TODO: In a future network upgrade, there will be only one unified wire format for both
// coreth and subnet-evm. At that point, this distinction between CorethLeafsRequest and
// SubnetEVMLeafsRequest will be removed.
type SubnetEVMLeafsRequest struct {
	Root     common.Hash `serialize:"true"`
	Account  common.Hash `serialize:"true"`
	Start    []byte      `serialize:"true"`
	End      []byte      `serialize:"true"`
	Limit    uint16      `serialize:"true"`
	NodeType NodeType
}

func (s SubnetEVMLeafsRequest) Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, handler RequestHandler) ([]byte, error) {
	return handler.HandleLeafsRequest(ctx, nodeID, requestID, s)
}

func (s SubnetEVMLeafsRequest) String() string {
	return fmt.Sprintf(
		"LeafsRequest(Root=%s, Account=%s, Start=%s, End=%s, Limit=%d, NodeType=%d)",
		s.Root, s.Account, common.Bytes2Hex(s.Start), common.Bytes2Hex(s.End), s.Limit, s.NodeType,
	)
}

func (s SubnetEVMLeafsRequest) RootHash() common.Hash    { return s.Root }
func (s SubnetEVMLeafsRequest) AccountHash() common.Hash { return s.Account }
func (s SubnetEVMLeafsRequest) StartKey() []byte         { return s.Start }
func (s SubnetEVMLeafsRequest) EndKey() []byte           { return s.End }
func (s SubnetEVMLeafsRequest) KeyLimit() uint16         { return s.Limit }
func (s SubnetEVMLeafsRequest) LeafType() NodeType       { return s.NodeType }
