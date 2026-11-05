// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

func newTestPublicKey(t *testing.T) []byte {
	t.Helper()

	sk, err := localsigner.New()
	require.NoError(t, err)
	return sk.PublicKey().Compress()
}

func TestParametersVerify(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	publicKey := newTestPublicKey(t)
	validValidators := []ValidatorInfo{{NodeID: nodeID, PublicKey: publicKey}}

	tests := []struct {
		name          string
		params        Parameters
		expectedError error
	}{
		{
			name: "valid",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: time.Second,
				InitialValidators:  validValidators,
			},
			expectedError: nil,
		},
		{
			name: "zero MaxNetworkDelay",
			params: Parameters{
				MaxNetworkDelay:    0,
				MaxRebroadcastWait: time.Second,
				InitialValidators:  validValidators,
			},
			expectedError: ErrInvalidParameters,
		},
		{
			name: "zero MaxRebroadcastWait",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: 0,
				InitialValidators:  validValidators,
			},
			expectedError: ErrInvalidParameters,
		},
		{
			name: "empty InitialValidators",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: time.Second,
				InitialValidators:  []ValidatorInfo{},
			},
			expectedError: ErrInvalidParameters,
		},
		{
			name: "nil InitialValidators",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: time.Second,
				InitialValidators:  nil,
			},
			expectedError: ErrInvalidParameters,
		},
		{
			name: "same public key on different nodes",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: time.Second,
				InitialValidators: []ValidatorInfo{
					{NodeID: nodeID, PublicKey: publicKey},
					{NodeID: ids.GenerateTestNodeID(), PublicKey: publicKey},
				},
			},
			expectedError: nil,
		},
		{
			name: "duplicate NodeID",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: time.Second,
				InitialValidators: []ValidatorInfo{
					{NodeID: nodeID, PublicKey: publicKey},
					{NodeID: nodeID, PublicKey: newTestPublicKey(t)},
				},
			},
			expectedError: errDuplicateNodeID,
		},
		{
			name: "missing public key",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: time.Second,
				InitialValidators:  []ValidatorInfo{{NodeID: nodeID}},
			},
			expectedError: bls.ErrFailedPublicKeyDecompress,
		},
		{
			name: "truncated public key after a valid validator",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: time.Second,
				InitialValidators: []ValidatorInfo{
					{NodeID: nodeID, PublicKey: publicKey},
					{NodeID: ids.GenerateTestNodeID(), PublicKey: publicKey[:bls.PublicKeyLen-1]},
				},
			},
			expectedError: bls.ErrFailedPublicKeyDecompress,
		},
		{
			// Decompresses successfully but fails key validation.
			name: "public key is the point at infinity",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: time.Second,
				InitialValidators: []ValidatorInfo{
					{NodeID: nodeID, PublicKey: append([]byte{0xc0}, make([]byte, bls.PublicKeyLen-1)...)},
				},
			},
			expectedError: ErrInvalidParameters,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.params.Verify()
			require.ErrorIs(t, err, test.expectedError)
			if test.expectedError != nil {
				require.ErrorIs(t, err, ErrInvalidParameters)
			}
		})
	}
}
