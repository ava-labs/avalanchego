// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

type mockClient struct {
	require        *require.Assertions
	expectedMethod string
	onSendRequestF func(reply interface{}) error
}

func (mc *mockClient) SendRequest(_ context.Context, method string, _ interface{}, reply interface{}, _ ...rpc.Option) error {
	mc.require.Equal(mc.expectedMethod, method)
	return mc.onSendRequestF(reply)
}

func TestIndexClient(t *testing.T) {
	require := require.New(t)
	client := client{}
	{
		// Test GetIndex
		client.requester = &mockClient{
			require:        require,
			expectedMethod: "index.getIndex",
			onSendRequestF: func(reply interface{}) error {
				*(reply.(*GetIndexResponse)) = GetIndexResponse{Index: 5}
				return nil
			},
		}
		index, err := client.GetIndex(context.Background(), ids.Empty)
		require.NoError(err)
		require.Equal(uint64(5), index)
	}
	{
		// Test GetLastAccepted
		id := ids.GenerateTestID()
		bytes := utils.RandomBytes(10)
		bytesStr, err := formatting.Encode(formatting.Hex, bytes)
		require.NoError(err)
		client.requester = &mockClient{
			require:        require,
			expectedMethod: "index.getLastAccepted",
			onSendRequestF: func(reply interface{}) error {
				*(reply.(*FormattedContainer)) = FormattedContainer{
					ID:    id,
					Bytes: bytesStr,
					Index: json.Uint64(10),
				}
				return nil
			},
		}
		container, index, err := client.GetLastAccepted(context.Background())
		require.NoError(err)
		require.Equal(id, container.ID)
		require.Equal(bytes, container.Bytes)
		require.Equal(uint64(10), index)
	}
	{
		// Test GetContainerRange
		id := ids.GenerateTestID()
		bytes := utils.RandomBytes(10)
		bytesStr, err := formatting.Encode(formatting.Hex, bytes)
		require.NoError(err)
		client.requester = &mockClient{
			require:        require,
			expectedMethod: "index.getContainerRange",
			onSendRequestF: func(reply interface{}) error {
				*(reply.(*GetContainerRangeResponse)) = GetContainerRangeResponse{Containers: []FormattedContainer{{
					ID:    id,
					Bytes: bytesStr,
				}}}
				return nil
			},
		}
		containers, err := client.GetContainerRange(context.Background(), 1, 10)
		require.NoError(err)
		require.Len(containers, 1)
		require.Equal(id, containers[0].ID)
		require.Equal(bytes, containers[0].Bytes)
	}
	{
		// Test IsAccepted
		client.requester = &mockClient{
			require:        require,
			expectedMethod: "index.isAccepted",
			onSendRequestF: func(reply interface{}) error {
				*(reply.(*IsAcceptedResponse)) = IsAcceptedResponse{IsAccepted: true}
				return nil
			},
		}
		isAccepted, err := client.IsAccepted(context.Background(), ids.Empty)
		require.NoError(err)
		require.True(isAccepted)
	}
	{
		// Test GetContainerByID
		id := ids.GenerateTestID()
		bytes := utils.RandomBytes(10)
		bytesStr, err := formatting.Encode(formatting.Hex, bytes)
		require.NoError(err)
		client.requester = &mockClient{
			require:        require,
			expectedMethod: "index.getContainerByID",
			onSendRequestF: func(reply interface{}) error {
				*(reply.(*FormattedContainer)) = FormattedContainer{
					ID:    id,
					Bytes: bytesStr,
					Index: json.Uint64(10),
				}
				return nil
			},
		}
		container, index, err := client.GetContainerByID(context.Background(), id)
		require.NoError(err)
		require.Equal(id, container.ID)
		require.Equal(bytes, container.Bytes)
		require.Equal(uint64(10), index)
	}
}
