package proposervm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmanmock"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type mockVM struct {
	response interface{}
	err      error
}

func (m *mockVM) GetBlock(_ context.Context, _ ids.ID) (snowman.Block, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.response.(snowman.Block), nil
}

func (m *mockVM) GetLastAcceptedHeight() uint64 {
	if m.err != nil {
		return 0
	}

	return m.response.(uint64)
}

func TestServiceGetProposedHeight(t *testing.T) {
	require := require.New(t)

	mockResponse := uint64(42)
	mockVM := &mockVM{response: mockResponse, err: nil}
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	proposerAPI := &ProposerAPI{ctx: snowCtx, vm: mockVM}

	reply := &api.GetHeightResponse{}
	err := proposerAPI.GetProposedHeight(nil, nil, reply)
	require.NoError(err)
	require.Equal(mockResponse, uint64(reply.Height))
}

func TestServiceGetProposerBlockWrapper(t *testing.T) {
	tests := []struct {
		name      string
		encoding  formatting.Encoding
		mockError error
	}{
		{
			name:     "successful response",
			encoding: formatting.Hex,
		},
		{
			name:      "error response",
			mockError: errors.New("block not found"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			snowBlock := snowmanmock.NewBlock(gomock.NewController(t))
			snowCtx := snowtest.Context(t, snowtest.CChainID)

			mockVM := &mockVM{
				response: snowBlock,
				err:      test.mockError,
			}

			proposerAPI := &ProposerAPI{ctx: snowCtx, vm: mockVM}

			snowBlock.EXPECT().Bytes().Return([]byte{1, 2, 3}).AnyTimes()
			encodedBlock, err := formatting.Encode(test.encoding, snowBlock.Bytes())
			require.NoError(err)
			expectedBlock, err := json.Marshal(encodedBlock)
			require.NoError(err)

			args := &GetProposerBlockArgs{
				ProposerBlockID: ids.ID{1},
				Encoding:        test.encoding,
			}
			reply := &api.GetBlockResponse{}

			err = proposerAPI.GetProposerBlockWrapper(&http.Request{}, args, reply)
			if test.mockError != nil {
				require.ErrorIs(err, test.mockError)
				return
			}

			require.NoError(err)
			require.NotNil(reply.Block)
			require.Equal(json.RawMessage(expectedBlock), reply.Block)
		})
	}
}
