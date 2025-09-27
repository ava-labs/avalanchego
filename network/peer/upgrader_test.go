// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
	"golang.org/x/sync/errgroup"

	_ "embed"

	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/staking"
)

// 8192RSA_test.pem is used here because it's too expensive
// to generate an 8K bit RSA key under the time constraint of the weak Github CI runners.

//go:embed 8192RSA_test.pem
var fat8192BitRSAKey []byte

func TestBlockClientsWithIncorrectRSAKeys(t *testing.T) {
	for _, testCase := range []struct {
		description      string
		genClientTLSCert func() tls.Certificate
		expectedErr      error
	}{
		{
			description: "Proper key size and private key - 2048",
			genClientTLSCert: func() tls.Certificate {
				privKey2048, err := rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)
				clientCert2048 := makeTLSCert(t, privKey2048)
				return clientCert2048
			},
		},
		{
			description: "Proper key size and private key - 4096",
			genClientTLSCert: func() tls.Certificate {
				privKey4096, err := rsa.GenerateKey(rand.Reader, 4096)
				require.NoError(t, err)
				clientCert4096 := makeTLSCert(t, privKey4096)
				return clientCert4096
			},
		},
		{
			description: "Too big key",
			genClientTLSCert: func() tls.Certificate {
				block, _ := pem.Decode(fat8192BitRSAKey)
				require.NotNil(t, block)
				rsaFatKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
				require.NoError(t, err)
				privKey8192 := rsaFatKey.(*rsa.PrivateKey)
				// Sanity check - ensure privKey8192 is indeed an 8192 RSA key
				require.Equal(t, 8192, privKey8192.N.BitLen())
				clientCert8192 := makeTLSCert(t, privKey8192)
				return clientCert8192
			},
			expectedErr: staking.ErrUnsupportedRSAModulusBitLen,
		},
		{
			description: "Improper public exponent",
			genClientTLSCert: func() tls.Certificate {
				clientCertBad := makeTLSCert(t, nonStandardRSAKey(t))
				return clientCertBad
			},
			expectedErr: staking.ErrUnsupportedRSAPublicExponent,
		},
	} {
		t.Run(testCase.description, func(t *testing.T) {
			serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
			require.NoError(t, err)

			serverCert := makeTLSCert(t, serverKey)

			config := peer.TLSConfig(serverCert, nil)

			c := prometheus.NewCounter(prometheus.CounterOpts{})

			// Initialize upgrader with a mock that fails when it's incremented.
			failOnIncrementCounter := &mockPrometheusCounter{
				Counter: c,
				onIncrement: func() {
					require.FailNow(t, "should not have invoked")
				},
			}
			upgrader := peer.NewTLSServerUpgrader(config, failOnIncrementCounter)

			clientConfig := tls.Config{
				ClientAuth:         tls.RequireAnyClientCert,
				InsecureSkipVerify: true, //#nosec G402
				MinVersion:         tls.VersionTLS13,
				Certificates:       []tls.Certificate{testCase.genClientTLSCert()},
			}

			listener, err := (&net.ListenConfig{}).Listen(
				context.Background(),
				"tcp",
				"127.0.0.1:0",
			)
			require.NoError(t, err)
			defer listener.Close()

			eg := &errgroup.Group{}
			eg.Go(func() error {
				conn, err := listener.Accept()
				if err != nil {
					return err
				}

				_, _, _, err = upgrader.Upgrade(context.Background(), conn)
				return err
			})

			conn, err := (&tls.Dialer{Config: &clientConfig}).DialContext(
				context.Background(),
				"tcp",
				listener.Addr().String(),
			)
			require.NoError(t, err)
			tlsConn, ok := conn.(*tls.Conn)
			require.True(t, ok)
			require.NoError(t, tlsConn.HandshakeContext(context.Background()))

			err = eg.Wait()
			require.ErrorIs(t, err, testCase.expectedErr)
		})
	}
}

func nonStandardRSAKey(t *testing.T) *rsa.PrivateKey {
	for {
		sk, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		// This speeds up RSA operations, and was initialized during the key-gen.
		// If we wish to override the key parameters we need to nullify this,
		// otherwise the signer will use these values and the verifier will use
		// the values we override, and verification will fail.
		sk.Precomputed = rsa.PrecomputedValues{}

		// We want a non-standard E, so let's use E = 257 and derive D again.
		e := 257
		sk.PublicKey.E = e
		sk.E = e

		p := sk.Primes[0]
		q := sk.Primes[1]

		pminus1 := new(big.Int).Sub(p, big.NewInt(1))
		qminus1 := new(big.Int).Sub(q, big.NewInt(1))

		phiN := big.NewInt(0).Mul(pminus1, qminus1)

		sk.D = big.NewInt(0).ModInverse(big.NewInt(int64(e)), phiN)

		if sk.D == nil {
			// If we ended up picking a bad starting modulus, try again.
			continue
		}

		return sk
	}
}

func makeTLSCert(t *testing.T, privKey *rsa.PrivateKey) tls.Certificate {
	x509Cert := makeCert(t, privKey, &privKey.PublicKey)

	rawX509PEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: x509Cert.Raw})
	privateKeyInDER, err := x509.MarshalPKCS8PrivateKey(privKey)
	require.NoError(t, err)

	privateKeyInPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyInDER})

	tlsCertServer, err := tls.X509KeyPair(rawX509PEM, privateKeyInPEM)
	require.NoError(t, err)

	return tlsCertServer
}

func makeCert(t *testing.T, privateKey any, publicKey any) *x509.Certificate {
	// Create a self-signed cert
	basicCert := basicCert()
	certBytes, err := x509.CreateCertificate(rand.Reader, basicCert, basicCert, publicKey, privateKey)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(certBytes)
	require.NoError(t, err)

	return cert
}

func basicCert() *x509.Certificate {
	return &x509.Certificate{
		SerialNumber:          big.NewInt(0).SetInt64(100),
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour).UTC(),
		BasicConstraintsValid: true,
	}
}

type mockPrometheusCounter struct {
	prometheus.Counter
	onIncrement func()
}

func (m *mockPrometheusCounter) Inc() {
	m.onIncrement()
}
