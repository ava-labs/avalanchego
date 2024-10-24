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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/network/peer"
)

func TestValidateRSACertificate(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	x509Cert := makeRSACertAndKey(t, key)

	x509CertWithNoPK := makeRSACertAndKey(t, key)
	x509CertWithNoPK.cert.PublicKey = nil

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	basicCert := basicCert()
	certBytes, err := x509.CreateCertificate(rand.Reader, basicCert, basicCert, &ecKey.PublicKey, ecKey)
	require.NoError(t, err)

	ecCert, err := x509.ParseCertificate(certBytes)
	require.NoError(t, err)

	for _, testCase := range []struct {
		description string
		input       tls.ConnectionState
		expectedErr error
	}{
		{
			description: "Valid TLS cert",
			input:       tls.ConnectionState{PeerCertificates: []*x509.Certificate{&x509Cert.cert}},
		},
		{
			description: "No TLS certs given",
			input:       tls.ConnectionState{},
			expectedErr: errors.New("no certificates sent by peer"),
		},
		{
			description: "No TLS certs given",
			input:       tls.ConnectionState{PeerCertificates: []*x509.Certificate{nil}},
			expectedErr: errors.New("certificate sent by peer is empty"),
		},
		{
			description: "No public key in the cert given",
			input:       tls.ConnectionState{PeerCertificates: []*x509.Certificate{&x509CertWithNoPK.cert}},
			expectedErr: errors.New("no public key sent by peer"),
		},
		{
			description: "EC cert",
			input:       tls.ConnectionState{PeerCertificates: []*x509.Certificate{ecCert}},
		},
	} {
		t.Run(testCase.description, func(t *testing.T) {
			require.Equal(t, testCase.expectedErr, peer.ValidateRSACertificate(testCase.input))
		})
	}
}
