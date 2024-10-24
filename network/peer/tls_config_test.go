// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/network/peer"
)

func TestValidateRSACertificate(t *testing.T) {
	for _, testCase := range []struct {
		description string
		input       func() tls.ConnectionState
		expectedErr error
	}{
		{
			description: "Valid TLS cert",
			input: func() tls.ConnectionState {
				key, err := rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)
				x509Cert := makeRSACertAndKey(t, key)
				return tls.ConnectionState{PeerCertificates: []*x509.Certificate{&x509Cert.cert}}
			},
		},
		{
			description: "No TLS certs given",
			input: func() tls.ConnectionState {
				return tls.ConnectionState{}
			},
			expectedErr: peer.ErrNoCertsSent,
		},
		{
			description: "Empty certificate given by peer",
			input: func() tls.ConnectionState {
				return tls.ConnectionState{PeerCertificates: []*x509.Certificate{nil}}
			},
			expectedErr: peer.ErrEmptyCert,
		},
		{
			description: "No public key in the cert given",
			input: func() tls.ConnectionState {
				key, err := rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)

				x509CertWithNoPK := makeRSACertAndKey(t, key)
				x509CertWithNoPK.cert.PublicKey = nil
				return tls.ConnectionState{PeerCertificates: []*x509.Certificate{&x509CertWithNoPK.cert}}
			},
			expectedErr: peer.ErrEmptyPublicKey,
		},
		{
			description: "EC cert",
			input: func() tls.ConnectionState {
				ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				require.NoError(t, err)

				basicCert := basicCert()
				certBytes, err := x509.CreateCertificate(rand.Reader, basicCert, basicCert, &ecKey.PublicKey, ecKey)
				require.NoError(t, err)

				ecCert, err := x509.ParseCertificate(certBytes)
				require.NoError(t, err)
				return tls.ConnectionState{PeerCertificates: []*x509.Certificate{ecCert}}
			},
		},
	} {
		t.Run(testCase.description, func(t *testing.T) {
			require.Equal(t, testCase.expectedErr, peer.ValidateRSACertificate(testCase.input()))
		})
	}
}
