// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"golang.org/x/crypto/ed25519"

	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/staking"
)

func TestValidateCertificate(t *testing.T) {
	for _, testCase := range []struct {
		description string
		input       func(t *testing.T) tls.ConnectionState
		expectedErr error
	}{
		{
			description: "Valid TLS cert",
			input: func(t *testing.T) tls.ConnectionState {
				key, err := rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)
				x509Cert := makeCert(t, key, &key.PublicKey)
				return tls.ConnectionState{PeerCertificates: []*x509.Certificate{x509Cert}}
			},
		},
		{
			description: "No TLS certs given",
			input: func(*testing.T) tls.ConnectionState {
				return tls.ConnectionState{}
			},
			expectedErr: peer.ErrNoCertsSent,
		},
		{
			description: "Empty certificate given by peer",
			input: func(*testing.T) tls.ConnectionState {
				return tls.ConnectionState{PeerCertificates: []*x509.Certificate{nil}}
			},
			expectedErr: peer.ErrEmptyCert,
		},
		{
			description: "nil RSA key",
			input: func(t *testing.T) tls.ConnectionState {
				key, err := rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)

				x509CertWithNilPK := makeCert(t, key, &key.PublicKey)
				x509CertWithNilPK.PublicKey = (*rsa.PublicKey)(nil)
				return tls.ConnectionState{PeerCertificates: []*x509.Certificate{x509CertWithNilPK}}
			},
			expectedErr: staking.ErrInvalidRSAPublicKey,
		},
		{
			description: "No public key in the cert given",
			input: func(t *testing.T) tls.ConnectionState {
				key, err := rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)

				x509CertWithNoPK := makeCert(t, key, &key.PublicKey)
				x509CertWithNoPK.PublicKey = nil
				return tls.ConnectionState{PeerCertificates: []*x509.Certificate{x509CertWithNoPK}}
			},
			expectedErr: peer.ErrUnsupportedKeyType,
		},
		{
			description: "EC cert",
			input: func(t *testing.T) tls.ConnectionState {
				ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				require.NoError(t, err)

				ecCert := makeCert(t, ecKey, &ecKey.PublicKey)

				require.NoError(t, err)
				return tls.ConnectionState{PeerCertificates: []*x509.Certificate{ecCert}}
			},
		},
		{
			description: "EC cert with empty key",
			expectedErr: peer.ErrEmptyPublicKey,
			input: func(t *testing.T) tls.ConnectionState {
				ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				require.NoError(t, err)

				ecCert := makeCert(t, ecKey, &ecKey.PublicKey)
				ecCert.PublicKey = (*ecdsa.PublicKey)(nil)

				return tls.ConnectionState{PeerCertificates: []*x509.Certificate{ecCert}}
			},
		},
		{
			description: "EC cert with P384 curve",
			expectedErr: peer.ErrCurveMismatch,
			input: func(t *testing.T) tls.ConnectionState {
				ecKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
				require.NoError(t, err)

				ecCert := makeCert(t, ecKey, &ecKey.PublicKey)

				return tls.ConnectionState{PeerCertificates: []*x509.Certificate{ecCert}}
			},
		},
		{
			description: "EC cert with ed25519 key not supported",
			expectedErr: peer.ErrUnsupportedKeyType,
			input: func(t *testing.T) tls.ConnectionState {
				pub, priv, err := ed25519.GenerateKey(rand.Reader)
				require.NoError(t, err)

				basicCert := basicCert()
				certBytes, err := x509.CreateCertificate(rand.Reader, basicCert, basicCert, pub, priv)
				require.NoError(t, err)

				ecCert, err := x509.ParseCertificate(certBytes)
				require.NoError(t, err)

				return tls.ConnectionState{PeerCertificates: []*x509.Certificate{ecCert}}
			},
		},
	} {
		t.Run(testCase.description, func(t *testing.T) {
			require.Equal(t, testCase.expectedErr, peer.ValidateCertificate(testCase.input(t)))
		})
	}
}
