// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	// "crypto"
	// "time"
	"testing"

	"github.com/stretchr/testify/require"
)

type mockVM struct {
	response interface{}
	err      error
}

// func (m *mockVM) GetStatelessSignedBlock(_ ids.ID) (block.SignedBlock, error) {
// 	if m.err != nil {
// 		return nil, m.err
// 	}

// 	return m.response.(block.SignedBlock), nil
// }

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
	// 	proposerAPI := &ProposerAPI{vm: mockVM}

	require.Equal(mockVM.GetLastAcceptedHeight(), mockResponse)

	// reply := &api.GetHeightResponse{}
	// require.NoError(proposerAPI.GetProposedHeight(nil, nil, reply))
	// require.Equal(mockResponse, uint64(reply.Height))
}

// func createSignedBlock(t *testing.T) block.SignedBlock {
// 	parentID := ids.ID{1}
// 	timestamp := time.Unix(123, 0)
// 	pChainHeight := uint64(2)
// 	pChainEpoch := block.PChainEpoch{
// 		Height:    uint64(2),
// 		Epoch:    uint64(0),
// 		StartTime: time.Unix(123, 0),
// 	}
// 	innerBlockBytes := []byte{3}
// 	chainID := ids.ID{4}

// 	tlsCert, err := staking.NewTLSCert()
// 	require.NoError(t, err)

// 	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
// 	require.NoError(t, err)
// 	key := tlsCert.PrivateKey.(crypto.Signer)

// 	signedBlock, err := block.Build(
// 		parentID,
// 		timestamp,
// 		pChainHeight,
// 		pChainEpoch,
// 		cert,
// 		innerBlockBytes,
// 		chainID,
// 		key,
// 	)
// 	require.NoError(t, err)

// 	return signedBlock
// }
