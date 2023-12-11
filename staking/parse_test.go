// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"testing"

	_ "embed"

	"github.com/stretchr/testify/require"
)

var (
	//go:embed large_rsa_key.cert
	largeRSAKeyCert []byte

	parsers = []struct {
		name  string
		parse func([]byte) (*Certificate, error)
	}{
		{
			name:  "ParseCertificate",
			parse: ParseCertificate,
		},
		{
			name:  "ParseCertificatePermissive",
			parse: ParseCertificatePermissive,
		},
	}
)

func TestParseCheckLargePublicKey(t *testing.T) {
	for _, parser := range parsers {
		t.Run(parser.name, func(t *testing.T) {
			_, err := parser.parse(largeRSAKeyCert)
			require.ErrorIs(t, err, ErrInvalidRSAPublicKey)
		})
	}
}

func BenchmarkParse(b *testing.B) {
	tlsCert, err := NewTLSCert()
	require.NoError(b, err)

	bytes := tlsCert.Leaf.Raw
	for _, parser := range parsers {
		b.Run(parser.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, err = parser.parse(bytes)
				require.NoError(b, err)
			}
		})
	}
}

func FuzzParseCertificate(f *testing.F) {
	f.Add(largeRSAKeyCert)
	f.Fuzz(func(t *testing.T, certBytes []byte) {
		{
			strictCert, err := ParseCertificate(certBytes)
			if err == nil {
				permissiveCert, err := ParseCertificatePermissive(certBytes)
				require.NoError(t, err)
				require.Equal(t, strictCert, permissiveCert)
			}
		}

		{
			_, err := ParseCertificatePermissive(certBytes)
			if err != nil {
				_, err = ParseCertificate(certBytes)
				require.Error(t, err) //nolint:forbidigo
			}
		}
	})
}
