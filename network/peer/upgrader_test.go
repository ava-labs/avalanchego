// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	_ "embed"

	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/staking"
)

// 8192RSA.pem is used here because it's too expensive
// to generate an 8K bit RSA key under the time constraint of the weak Github CI runners.

//go:embed 8192RSA.pem
var fat8192BitRSAKey []byte

func TestBlockClientsWithIncorrectRSAKeys(t *testing.T) {
	privKey2048, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	privKey4096, err := rsa.GenerateKey(rand.Reader, 4096)
	require.NoError(t, err)

	block, _ := pem.Decode(fat8192BitRSAKey)
	require.NotNil(t, block)

	rsaFatKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)

	privKey8192 := rsaFatKey.(*rsa.PrivateKey)

	// Sanity check - ensure privKey8192 is indeed an 8192 RSA key
	require.Equal(t, 8192, privKey8192.N.BitLen())

	clientCert2048 := makeTLSCert(t, privKey2048)
	clientCert4096 := makeTLSCert(t, privKey4096)
	clientCert8192 := makeTLSCert(t, privKey8192)
	clientCertBad := makeTLSCert(t, nonStandardRSAKey(t))

	for _, testCase := range []struct {
		description   string
		clientTLSCert tls.Certificate
		shouldSucceed bool
		expectedErr   error
	}{
		{
			description:   "Proper key size and private key - 2048",
			clientTLSCert: clientCert2048,
			shouldSucceed: true,
		},
		{
			description:   "Proper key size and private key - 4096",
			clientTLSCert: clientCert4096,
			shouldSucceed: true,
		},
		{
			description:   "Too big key",
			clientTLSCert: clientCert8192,
			expectedErr:   staking.ErrUnsupportedRSAModulusBitLen,
		},
		{
			description:   "Improper public exponent",
			clientTLSCert: clientCertBad,
			expectedErr:   staking.ErrUnsupportedRSAPublicExponent,
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
				t:       t,
				onIncrement: func() {
					require.FailNow(t, "should not have invoked")
				},
			}
			upgrader := peer.NewTLSServerUpgrader(config, failOnIncrementCounter)

			clientConfig := tls.Config{
				ClientAuth:         tls.RequireAnyClientCert,
				InsecureSkipVerify: true, //#nosec G402
				MinVersion:         tls.VersionTLS13,
				Certificates:       []tls.Certificate{testCase.clientTLSCert},
			}

			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()

			var wg sync.WaitGroup
			wg.Add(1)

			go func() {
				defer wg.Done()
				conn, err := listener.Accept()
				require.NoError(t, err)

				_, _, _, err = upgrader.Upgrade(conn)

				if testCase.shouldSucceed {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, testCase.expectedErr)
				}
			}()

			conn, err := tls.Dial("tcp", listener.Addr().String(), &clientConfig)
			require.NoError(t, err)

			require.NoError(t, conn.Handshake())

			wg.Wait()
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
	x509Cert := makeRSACertAndKey(t, privKey)

	rawX509PEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: x509Cert.cert.Raw})
	privateKeyInDER, err := x509.MarshalPKCS8PrivateKey(x509Cert.key)
	require.NoError(t, err)

	privateKeyInPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyInDER})

	tlsCertServer, err := tls.X509KeyPair(rawX509PEM, privateKeyInPEM)
	require.NoError(t, err)

	return tlsCertServer
}

type certAndKey struct {
	cert x509.Certificate
	key  *rsa.PrivateKey
}

func makeRSACertAndKey(t *testing.T, privKey *rsa.PrivateKey) certAndKey {
	// Create a self-signed cert
	basicCert := basicCert()
	certBytes, err := x509.CreateCertificate(rand.Reader, basicCert, basicCert, &privKey.PublicKey, privKey)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(certBytes)
	require.NoError(t, err)

	return certAndKey{
		cert: *cert,
		key:  privKey,
	}
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
	t *testing.T
	prometheus.Counter
	onIncrement func()
}

func (m *mockPrometheusCounter) Inc() {
	m.onIncrement()
}
