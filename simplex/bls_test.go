// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

func TestBLSVerifier(t *testing.T) {
	config := newEngineConfig(t, 1)
	signer, verifier, err := NewBLSAuth(config)
	require.NoError(t, err)
	otherKey, err := localsigner.New()
	require.NoError(t, err)
	otherPK := otherKey.PublicKey().Compress()

	msg := []byte("Begin at the beginning, and go on till you come to the end: then stop")
	tests := []struct {
		name      string
		expectErr error
		pk        []byte
		sig       []byte
	}{
		{
			name:      "valid_signature",
			expectErr: nil,
			pk:        config.Params.InitialValidators[0].PublicKey,
			sig: func() []byte {
				sig, err := signer.Sign(msg)
				require.NoError(t, err)
				return sig
			}(),
		},
		{
			name:      "wrong_public_key",
			expectErr: errSignatureVerificationFailed,
			pk:        otherPK,
			sig: func() []byte {
				sig, err := signer.Sign(msg)
				require.NoError(t, err)
				return sig
			}(),
		},
		{
			name:      "invalid_message_encoding",
			expectErr: errSignatureVerificationFailed,
			pk:        config.Params.InitialValidators[0].PublicKey,
			sig: func() []byte {
				sig, err := config.SignBLS(msg)
				require.NoError(t, err)
				return bls.SignatureToBytes(sig)
			}(),
		},
		{
			name:      "malformed_public_key",
			expectErr: errFailedToParsePublicKey,
			pk:        []byte{0x01, 0x02, 0x03, 0x04, 0x05}, // Incorrect length PublicKey
			sig: func() []byte {
				sig, err := signer.Sign(msg)
				require.NoError(t, err)
				return sig
			}(),
		},
		{
			name:      "nil_signature",
			expectErr: errFailedToParseSignature,
			pk:        config.Params.InitialValidators[0].PublicKey,
			sig:       nil,
		},
		{
			name:      "malformed_signature",
			expectErr: errFailedToParseSignature,
			pk:        config.Params.InitialValidators[0].PublicKey,
			sig:       []byte{0x01, 0x02, 0x03}, // Malformed signature
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifier.VerifySignature(msg, tt.sig, tt.pk)
			require.ErrorIs(t, err, tt.expectErr)
		})
	}
}
