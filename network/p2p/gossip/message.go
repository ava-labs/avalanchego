// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/utils/bloom"
)

func MarshalAppRequest(filter, salt []byte) ([]byte, error) {
	request := &sdk.PullGossipRequest{
		Filter: filter,
		Salt:   salt,
	}
	return proto.Marshal(request)
}

func ParseAppRequest(bytes []byte) (*bloom.ReadFilter, ids.ID, error) {
	request := &sdk.PullGossipRequest{}
	if err := proto.Unmarshal(bytes, request); err != nil {
		return nil, ids.Empty, err
	}

	salt, err := ids.ToID(request.Salt)
	if err != nil {
		return nil, ids.Empty, err
	}

	filter, err := bloom.Parse(request.Filter)
	return filter, salt, err
}

func MarshalAppResponse(gossip [][]byte) ([]byte, error) {
	return proto.Marshal(&sdk.PullGossipResponse{
		Gossip: gossip,
	})
}

func ParseAppResponse(bytes []byte) ([][]byte, error) {
	response := &sdk.PullGossipResponse{}
	err := proto.Unmarshal(bytes, response)
	return response.Gossip, err
}

func MarshalAppGossip(gossip [][]byte) ([]byte, error) {
	return proto.Marshal(&sdk.PushGossip{
		Gossip: gossip,
	})
}

func ParseAppGossip(bytes []byte) ([][]byte, error) {
	msg := &sdk.PushGossip{}
	err := proto.Unmarshal(bytes, msg)
	return msg.Gossip, err
}
