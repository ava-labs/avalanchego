// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

type mockClient struct {
	response interface{}
	err      error
}

// NewMockClient returns a mock client for testing
func NewMockClient(response interface{}, err error) rpc.EndpointRequester {
	return &mockClient{
		response: response,
		err:      err,
	}
}

func (mc *mockClient) SendRequest(_ context.Context, _ string, _ interface{}, reply interface{}, _ ...rpc.Option) error {
	if mc.err != nil {
		return mc.err
	}

	switch p := reply.(type) {
	case *api.GetHeightResponse:
		response := mc.response.(*api.GetHeightResponse)
		*p = *response
	default:
		panic("illegal type")
	}
	return nil
}

func TestNewClient(t *testing.T) {
	require := require.New(t)

	c := NewClient("uri", "blockchainName")
	require.NotNil(c)
}

func TestGetProposedHeightJsonRPC(t *testing.T) {
	tests := []struct {
		name         string
		mockResponse *api.GetHeightResponse
		mockError    error
	}{
		{
			name:         "success",
			mockResponse: &api.GetHeightResponse{Height: 42},
			mockError:    nil,
		},
		{
			name:         "error",
			mockResponse: nil,
			mockError:    errors.New("test error"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			mc := NewMockClient(test.mockResponse, test.mockError)
			c := &client{requester: mc}

			height, err := c.GetProposedHeight(context.Background(), nil)
			if test.mockError != nil {
				require.ErrorIs(err, test.mockError)
				return
			}
			require.NoError(err)
			require.Equal(uint64(42), height)
		})
	}
}
