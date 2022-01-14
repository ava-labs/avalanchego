// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/stretchr/testify/assert"
)

type mockClient struct {
	assert         *assert.Assertions
	expectedMethod string
	onSendRequestF func(reply interface{}) error
}

func (mc *mockClient) SendRequest(ctx context.Context, method string, _ interface{}, reply interface{}) error {
	mc.assert.Equal(mc.expectedMethod, method)
	return mc.onSendRequestF(reply)
}

func TestIndexClient(t *testing.T) {
	assert := assert.New(t)
	client := client{}
	{
		// Test GetIndex
		client.requester = &mockClient{
			assert:         assert,
			expectedMethod: "getIndex",
			onSendRequestF: func(reply interface{}) error {
				*(reply.(*GetIndexResponse)) = GetIndexResponse{Index: 5}
				return nil
			},
		}
		index, err := client.GetIndex(context.Background(), &GetIndexArgs{ContainerID: ids.Empty, Encoding: formatting.Hex})
		assert.NoError(err)
		assert.EqualValues(5, index)
	}
	{
		// Test GetLastAccepted
		id := ids.GenerateTestID()
		client.requester = &mockClient{
			assert:         assert,
			expectedMethod: "getLastAccepted",
			onSendRequestF: func(reply interface{}) error {
				*(reply.(*FormattedContainer)) = FormattedContainer{ID: id}
				return nil
			},
		}
		container, err := client.GetLastAccepted(context.Background(), &GetLastAcceptedArgs{Encoding: formatting.Hex})
		assert.NoError(err)
		assert.EqualValues(id, container.ID)
	}
	{
		// Test GetContainerRange
		id := ids.GenerateTestID()
		client.requester = &mockClient{
			assert:         assert,
			expectedMethod: "getContainerRange",
			onSendRequestF: func(reply interface{}) error {
				*(reply.(*GetContainerRangeResponse)) = GetContainerRangeResponse{Containers: []FormattedContainer{{ID: id}}}
				return nil
			},
		}
		containers, err := client.GetContainerRange(context.Background(), &GetContainerRangeArgs{StartIndex: 1, NumToFetch: 10, Encoding: formatting.Hex})
		assert.NoError(err)
		assert.Len(containers, 1)
		assert.EqualValues(id, containers[0].ID)
	}
	{
		// Test IsAccepted
		client.requester = &mockClient{
			assert:         assert,
			expectedMethod: "isAccepted",
			onSendRequestF: func(reply interface{}) error {
				*(reply.(*bool)) = true
				return nil
			},
		}
		isAccepted, err := client.IsAccepted(context.Background(), &GetIndexArgs{ContainerID: ids.Empty, Encoding: formatting.Hex})
		assert.NoError(err)
		assert.True(isAccepted)
	}
	{
		// Test GetContainerByID
		id := ids.GenerateTestID()
		client.requester = &mockClient{
			assert:         assert,
			expectedMethod: "getContainerByID",
			onSendRequestF: func(reply interface{}) error {
				*(reply.(*FormattedContainer)) = FormattedContainer{ID: id}
				return nil
			},
		}
		container, err := client.GetContainerByID(context.Background(), &GetIndexArgs{ContainerID: id, Encoding: formatting.Hex})
		assert.NoError(err)
		assert.EqualValues(id, container.ID)
	}
}
