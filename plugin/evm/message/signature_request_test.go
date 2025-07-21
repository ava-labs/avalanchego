// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/stretchr/testify/require"
)

// TestMarshalMessageSignatureRequest asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalMessageSignatureRequest(t *testing.T) {
	signatureRequest := MessageSignatureRequest{
		MessageID: ids.ID{68, 79, 70, 65, 72, 73, 64, 107},
	}

	base64MessageSignatureRequest := "AABET0ZBSElAawAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
	signatureRequestBytes, err := Codec.Marshal(Version, signatureRequest)
	require.NoError(t, err)
	require.Equal(t, base64MessageSignatureRequest, base64.StdEncoding.EncodeToString(signatureRequestBytes))

	var s MessageSignatureRequest
	_, err = Codec.Unmarshal(signatureRequestBytes, &s)
	require.NoError(t, err)
	require.Equal(t, signatureRequest.MessageID, s.MessageID)
}

// TestMarshalBlockSignatureRequest asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalBlockSignatureRequest(t *testing.T) {
	signatureRequest := BlockSignatureRequest{
		BlockID: ids.ID{68, 79, 70, 65, 72, 73, 64, 107},
	}

	base64BlockSignatureRequest := "AABET0ZBSElAawAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
	signatureRequestBytes, err := Codec.Marshal(Version, signatureRequest)
	require.NoError(t, err)
	require.Equal(t, base64BlockSignatureRequest, base64.StdEncoding.EncodeToString(signatureRequestBytes))

	var s BlockSignatureRequest
	_, err = Codec.Unmarshal(signatureRequestBytes, &s)
	require.NoError(t, err)
	require.Equal(t, signatureRequest.BlockID, s.BlockID)
}

// TestMarshalSignatureResponse asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalSignatureResponse(t *testing.T) {
	var signature [bls.SignatureLen]byte
	sig, err := hex.DecodeString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err, "failed to decode string to hex")

	copy(signature[:], sig)
	signatureResponse := SignatureResponse{
		Signature: signature,
	}

	base64SignatureResponse := "AAABI0VniavN7wEjRWeJq83vASNFZ4mrze8BI0VniavN7wEjRWeJq83vASNFZ4mrze8BI0VniavN7wEjRWeJq83vASNFZ4mrze8BI0VniavN7wEjRWeJq83vASNFZ4mrze8="
	signatureResponseBytes, err := Codec.Marshal(Version, signatureResponse)
	require.NoError(t, err)
	require.Equal(t, base64SignatureResponse, base64.StdEncoding.EncodeToString(signatureResponseBytes))

	var s SignatureResponse
	_, err = Codec.Unmarshal(signatureResponseBytes, &s)
	require.NoError(t, err)
	require.Equal(t, signatureResponse.Signature, s.Signature)
}
