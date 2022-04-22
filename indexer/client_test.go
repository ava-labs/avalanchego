// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

type mockClient struct {
	assert         *assert.Assertions
	expectedMethod string
	onSendRequestF func(reply interface{}) error
}

func (mc *mockClient) SendRequest(ctx context.Context, method string, _ interface{}, reply interface{}, options ...rpc.Option) error {
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
		index, err := client.GetIndex(context.Background(), ids.Empty)
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
		container, err := client.GetLastAccepted(context.Background())
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
		containers, err := client.GetContainerRange(context.Background(), 1, 10)
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
				*(reply.(*IsAcceptedResponse)) = IsAcceptedResponse{IsAccepted: true}
				return nil
			},
		}
		isAccepted, err := client.IsAccepted(context.Background(), ids.Empty)
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
		container, err := client.GetContainerByID(context.Background(), id)
		assert.NoError(err)
		assert.EqualValues(id, container.ID)
	}
}
