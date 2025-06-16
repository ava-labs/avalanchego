// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func TestBLSVerifier(t *testing.T) {
	config, err := newEngineConfig()
	require.NoError(t, err)
	signer, verifier := NewBLSAuth(config)
	otherNodeID := ids.GenerateTestNodeID()

	msg := []byte("Begin at the beginning, and go on till you come to the end: then stop")
	tests := []struct {
		name      string
		expectErr error
		nodeID    []byte
		sig       []byte
	}{
		{
			name:      "valid signature",
			expectErr: nil,
			nodeID:    config.Ctx.NodeID[:],
			sig: func() []byte {
				sig, err := signer.Sign(msg)
				require.NoError(t, err)
				return sig
			}(),
		},
		{
			name:      "not in membership set",
			expectErr: errSignerNotFound,
			nodeID:    otherNodeID[:],
			sig: func() []byte {
				sig, err := signer.Sign(msg)
				require.NoError(t, err)
				return sig
			}(),
		},
		{
			name:      "invalid encoding",
			expectErr: errSignatureVerificationFailed,
			nodeID:    config.Ctx.NodeID[:],
			sig: func() []byte {
				sig, err := config.SignBLS(msg)
				require.NoError(t, err)
				return bls.SignatureToBytes(sig)
			}(),
		},
		{
			name:      "nodeID incorrect length",
			expectErr: errInvalidNodeIDLength,
			nodeID:    []byte{0x01, 0x02, 0x03, 0x04, 0x05}, // Incorrect length NodeID
			sig: func() []byte {
				sig, err := signer.Sign(msg)
				require.NoError(t, err)
				return sig
			}(),
		},
		{
			name:      "nil signature",
			expectErr: errFailedToParseSignature,
			nodeID:    config.Ctx.NodeID[:],
			sig:       nil,
		},
		{
			name:      "malformed signature",
			expectErr: errFailedToParseSignature,
			nodeID:    config.Ctx.NodeID[:],
			sig:       []byte{0x01, 0x02, 0x03}, // Malformed signature
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err = verifier.Verify(msg, tt.sig, tt.nodeID)
			require.ErrorIs(t, err, tt.expectErr)
		})
	}
}
