// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"crypto"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

type mockVM struct {
	response interface{}
	err      error
}

func (m *mockVM) GetStatelessSignedBlock(_ ids.ID) (block.SignedBlock, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.response.(block.SignedBlock), nil
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
	require.NoError(proposerAPI.GetProposedHeight(nil, nil, reply))
	require.Equal(mockResponse, uint64(reply.Height))
}

func TestServiceGetProposerBlockWrapper(t *testing.T) {
	tests := []struct {
		name      string
		encoding  formatting.Encoding
		mockError error
	}{
		{
			name:     "successful response with JSON encoding",
			encoding: formatting.JSON,
		},
		{
			name:     "successful response with HEX encoding",
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

			signedBlock := createSignedBlock(t)
			mockVM := &mockVM{
				response: signedBlock,
				err:      test.mockError,
			}

			proposerAPI := &ProposerAPI{ctx: snowtest.Context(t, snowtest.CChainID), vm: mockVM}

			var expectedBlock json.RawMessage
			var err error
			switch test.encoding {
			case formatting.JSON:
				expectedBlock, err = json.Marshal(signedBlock)
				require.NoError(err)
			default:
				encodedBlock, err := formatting.Encode(test.encoding, signedBlock.Bytes())
				require.NoError(err)
				expectedBlock, err = json.Marshal(encodedBlock)
				require.NoError(err)
			}

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
			require.JSONEq(string(expectedBlock), string(reply.Block))
		})
	}
}

func createSignedBlock(t *testing.T) block.SignedBlock {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	pChainEpochHeight := uint64(2)
	epochNumber := uint64(0)
	epochStartTime := time.Unix(123, 0)
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(t, err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	signedBlock, err := block.Build(
		parentID,
		timestamp,
		pChainHeight,
		pChainEpochHeight,
		epochNumber,
		epochStartTime,
		cert,
		innerBlockBytes,
		chainID,
		key,
	)
	require.NoError(t, err)

	return signedBlock
}
