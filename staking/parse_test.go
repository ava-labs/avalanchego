// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto/rand"
	"crypto/rsa"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	_ "embed"
)

//go:embed large_rsa_key.cert
var largeRSAKeyCert []byte

func TestParseCheckLargeCert(t *testing.T) {
	_, err := ParseCertificate(largeRSAKeyCert)
	require.ErrorIs(t, err, ErrCertificateTooLarge)
}

func BenchmarkParse(b *testing.B) {
	tlsCert, err := NewTLSCert()
	require.NoError(b, err)

	bytes := tlsCert.Leaf.Raw

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = ParseCertificate(bytes)
		require.NoError(b, err)
	}
}

func TestValidateRSAPublicKeyIsWellFormed(t *testing.T) {
	validKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	minusN := new(big.Int).Set(validKey.N)
	minusN.Neg(minusN)

	evenN := new(big.Int).Set(validKey.N)
	evenN.Add(evenN, big.NewInt(1))

	bigMod := new(big.Int).Set(validKey.N)
	bigMod.Lsh(bigMod, 2049)

	for _, testCase := range []struct {
		description string
		expectErr   error
		pk          rsa.PublicKey
	}{
		{
			description: "valid public key",
			pk:          validKey.PublicKey,
		},
		{
			description: "even modulus",
			pk: rsa.PublicKey{
				N: evenN,
				E: 65537,
			},
			expectErr: ErrRSAModulusIsEven,
		},
		{
			description: "unsupported exponent",
			expectErr:   ErrUnsupportedRSAPublicExponent,
			pk: rsa.PublicKey{
				N: validKey.N,
				E: 3,
			},
		},
		{
			description: "unsupported modulus bit len",
			expectErr:   ErrUnsupportedRSAModulusBitLen,
			pk: rsa.PublicKey{
				N: bigMod,
				E: 65537,
			},
		},
		{
			description: "not positive modulus",
			expectErr:   ErrRSAModulusNotPositive,
			pk: rsa.PublicKey{
				N: minusN,
				E: 65537,
			},
		},
	} {
		t.Run(testCase.description, func(t *testing.T) {
			err := ValidateRSAPublicKeyIsWellFormed(&testCase.pk)
			require.ErrorIs(t, err, testCase.expectErr)
		})
	}
}
