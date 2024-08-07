// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var localStakingPath = "../../../staking/local/"

func loadTLSCertFromFiles(keyPath, certPath string) (*tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	return &cert, err
}

func newCertAndKeyBytesWithNoExt() ([]byte, []byte, error) {
	// Create RSA key to sign cert with
	rsaKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't generate rsa key: %w", err)
	}

	// Create self-signed staking cert
	certTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(0),
		NotBefore:             time.Date(2000, time.January, 0, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Now().AddDate(100, 0, 0),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageDataEncipherment,
		BasicConstraintsValid: true,
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, &rsaKey.PublicKey, rsaKey)
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't create certificate: %w", err)
	}
	var certBuff bytes.Buffer
	if err := pem.Encode(&certBuff, &pem.Block{Type: "CERTIFICATE", Bytes: certBytes}); err != nil {
		return nil, nil, fmt.Errorf("couldn't write cert file: %w", err)
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't marshal private key: %w", err)
	}

	var keyBuff bytes.Buffer
	if err := pem.Encode(&keyBuff, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		return nil, nil, fmt.Errorf("couldn't write private key: %w", err)
	}
	return certBuff.Bytes(), keyBuff.Bytes(), nil
}

func getPublicKey(t *testing.T, tlsCert *tls.Certificate) []byte {
	secp256Factory := Factory{}
	var nodePrivateKey *PrivateKey

	rsaPrivateKey, ok := tlsCert.PrivateKey.(*rsa.PrivateKey)
	require.True(t, ok)
	secpPrivateKey := RsaPrivateKeyToSecp256PrivateKey(rsaPrivateKey)
	nodePrivateKey, err := secp256Factory.ToPrivateKey(secpPrivateKey.Serialize())
	require.NoError(t, err)
	return nodePrivateKey.Address().Bytes()
}

func TestRecoverSecp256PublicKey(t *testing.T) {
	tests := []struct {
		name                  string
		generateCertAndPubKey func() (*x509.Certificate, []byte)
		want                  []byte
		wantErr               error
	}{
		{
			name: "Happy path",
			generateCertAndPubKey: func() (*x509.Certificate, []byte) {
				tlsCert, err := loadTLSCertFromFiles(localStakingPath+"staker1.key", localStakingPath+"staker1.crt")
				require.NoError(t, err)
				publicKey := getPublicKey(t, tlsCert)
				return tlsCert.Leaf, publicKey
			},
			wantErr: nil,
		},
		{
			name: "No node signature provided",
			generateCertAndPubKey: func() (*x509.Certificate, []byte) {
				certBytes, keyBytes, err := newCertAndKeyBytesWithNoExt()
				require.NoError(t, err)
				tlsCert, err := tls.X509KeyPair(certBytes, keyBytes)
				require.NoError(t, err)
				tlsCert.Leaf, err = x509.ParseCertificate(tlsCert.Certificate[0])
				require.NoError(t, err)
				publicKey := getPublicKey(t, &tlsCert)
				return tlsCert.Leaf, publicKey
			},
			wantErr: errNoSignature,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cert, want := tt.generateCertAndPubKey()
			got, err := RecoverSecp256PublicKey(cert)
			require.Equal(t, tt.wantErr, err)
			if err == nil {
				require.Equal(t, want, got)
			}
		})
	}
}
