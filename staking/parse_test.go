// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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

	for _, testCase := range []struct {
		description string
		expectErr   error
		getPK       func() rsa.PublicKey
	}{
		{
			description: "valid public key",
			getPK: func() rsa.PublicKey {
				return validKey.PublicKey
			},
		},
		{
			description: "even modulus",
			expectErr:   ErrRSAModulusIsEven,
			getPK: func() rsa.PublicKey {
				evenN := new(big.Int).Set(validKey.N)
				evenN.Add(evenN, big.NewInt(1))
				return rsa.PublicKey{
					N: evenN,
					E: 65537,
				}
			},
		},
		{
			description: "unsupported exponent",
			expectErr:   ErrUnsupportedRSAPublicExponent,
			getPK: func() rsa.PublicKey {
				return rsa.PublicKey{
					N: validKey.N,
					E: 3,
				}
			},
		},
		{
			description: "unsupported modulus bit len",
			expectErr:   ErrUnsupportedRSAModulusBitLen,
			getPK: func() rsa.PublicKey {
				bigMod := new(big.Int).Set(validKey.N)
				bigMod.Lsh(bigMod, 2049)
				return rsa.PublicKey{
					N: bigMod,
					E: 65537,
				}
			},
		},
		{
			description: "not positive modulus",
			expectErr:   ErrRSAModulusNotPositive,
			getPK: func() rsa.PublicKey {
				minusN := new(big.Int).Set(validKey.N)
				minusN.Neg(minusN)
				return rsa.PublicKey{
					N: minusN,
					E: 65537,
				}
			},
		},
	} {
		t.Run(testCase.description, func(t *testing.T) {
			pk := testCase.getPK()
			err := ValidateRSAPublicKeyIsWellFormed(&pk)
			require.ErrorIs(t, err, testCase.expectErr)
		})
	}
}
