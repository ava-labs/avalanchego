package proposervm

import (
	"context"
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/stretchr/testify/require"
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
	case *api.FormattedBlock:
		response := mc.response.(*api.FormattedBlock)
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

func TestGetProposedHeight(t *testing.T) {
	require := require.New(t)

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
			mockError:    fmt.Errorf("error"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			mc := NewMockClient(test.mockResponse, test.mockError)
			c := &client{requester: mc}

			height, err := c.GetProposedHeight(context.Background(), nil)
			if test.mockError != nil {
				require.Error(err)
				return
			}
			require.NoError(err)
			require.Equal(uint64(42), height)
		})
	}
}

func TestGetProposerBlockWrapper(t *testing.T) {
	require := require.New(t)

	tests := []struct {
		name         string
		mockResponse *api.FormattedBlock
		mockError    error
	}{
		{
			name:         "success",
			mockResponse: &api.FormattedBlock{Encoding: formatting.Hex, Block: "0x0017afa01d"},
			mockError:    nil,
		},
		{
			name:         "error",
			mockResponse: nil,
			mockError:    fmt.Errorf("error"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			mc := NewMockClient(test.mockResponse, test.mockError)
			c := &client{requester: mc}

			block, err := c.GetProposerBlockWrapper(context.Background(), ids.GenerateTestID())
			if test.mockError != nil {
				require.Error(err)
				return
			}
			require.NoError(err)
			require.Equal("\x00", string(block))
		})
	}
}
