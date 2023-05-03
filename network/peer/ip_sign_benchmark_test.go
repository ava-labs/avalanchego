// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"fmt"
	"net"
	"testing"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/ips"
)

// Benchmarks IP signing.
//
// e.g.,
//
//	$ go test -run=NONE -bench=BenchmarkIPSign
//	$ go test -run=NONE -bench=BenchmarkIPSign -benchmem
func BenchmarkIPSign(b *testing.B) {
	tt := []struct {
		keyPath  string
		certPath string
		desc     string
	}{
		{
			keyPath:  "test-certs/rsa-sha256.key",
			certPath: "test-certs/rsa-sha256.cert",
			desc:     "RSA-SHA256",
		},
		{
			keyPath:  "test-certs/ecdsa-sha256.key",
			certPath: "test-certs/ecdsa-sha256.cert",
			desc:     "ECDSA-SHA256",
		},
		{
			keyPath:  "test-certs/ecdsa-sha384.key",
			certPath: "test-certs/ecdsa-sha384.cert",
			desc:     "ECDSA-SHA384",
		},
	}
	b.ResetTimer()
	for _, tv := range tt {
		b.Run(fmt.Sprintf("sign with %q", tv.desc), func(b *testing.B) {
			benchmarkIPSign(b, tv.keyPath, tv.certPath)
		})
	}
}

func benchmarkIPSign(b *testing.B, keyPath string, certPath string) {
	tlsCert, err := staking.LoadTLSCertFromFiles(keyPath, certPath)
	if err != nil {
		b.Fatal(err)
	}

	signer, ok := tlsCert.PrivateKey.(crypto.Signer)
	if !ok {
		b.Fatalf("expected PrivateKey to be crypto.Signer, got %T", tlsCert.PrivateKey)
	}

	hashFunc, sigAlgo, err := deriveHash(tlsCert.Leaf.SignatureAlgorithm)
	if err != nil {
		b.Fatal(err)
	}

	unsignedIP := UnsignedIP{
		IPPort: ips.IPPort{
			IP:   net.IPv6zero,
			Port: 0,
		},
		Timestamp: 0,
	}
	for i := 0; i < b.N; i++ {
		if _, err := unsignedIP.Sign(signer, hashFunc, sigAlgo); err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmarks IP sign verify.
//
// e.g.,
//
//	$ go test -run=NONE -bench=BenchmarkIPVerify
//	$ go test -run=NONE -bench=BenchmarkIPVerify -benchmem
func BenchmarkIPVerify(b *testing.B) {
	tt := []struct {
		keyPath  string
		certPath string
		desc     string
	}{
		{
			keyPath:  "test-certs/rsa-sha256.key",
			certPath: "test-certs/rsa-sha256.cert",
			desc:     "RSA-SHA256",
		},
		{
			keyPath:  "test-certs/ecdsa-sha256.key",
			certPath: "test-certs/ecdsa-sha256.cert",
			desc:     "ECDSA-SHA256",
		},
		{
			keyPath:  "test-certs/ecdsa-sha384.key",
			certPath: "test-certs/ecdsa-sha384.cert",
			desc:     "ECDSA-SHA384",
		},
	}
	b.ResetTimer()
	for _, tv := range tt {
		b.Run(fmt.Sprintf("verify signed IP with %q", tv.desc), func(b *testing.B) {
			benchmarkIPVerify(b, tv.keyPath, tv.certPath)
		})
	}
}

func benchmarkIPVerify(b *testing.B, keyPath string, certPath string) {
	tlsCert, err := staking.LoadTLSCertFromFiles(keyPath, certPath)
	if err != nil {
		b.Fatal(err)
	}

	signer, ok := tlsCert.PrivateKey.(crypto.Signer)
	if !ok {
		b.Fatalf("expected PrivateKey to be crypto.Signer, got %T", tlsCert.PrivateKey)
	}

	hashFunc, sigAlgo, err := deriveHash(tlsCert.Leaf.SignatureAlgorithm)
	if err != nil {
		b.Fatal(err)
	}

	unsignedIP := UnsignedIP{
		IPPort: ips.IPPort{
			IP:   net.IPv6zero,
			Port: 0,
		},
		Timestamp: 0,
	}
	signedIP, err := unsignedIP.Sign(signer, hashFunc, sigAlgo)
	if err != nil {
		b.Fatal(err)
	}

	for i := 0; i < b.N; i++ {
		if err := signedIP.Verify(tlsCert.Leaf); err != nil {
			b.Fatal(err)
		}
	}
}
