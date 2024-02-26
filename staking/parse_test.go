// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"testing"

	"github.com/stretchr/testify/require"

	_ "embed"
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

func TestParseCheckLargeCert(t *testing.T) {
	for _, parser := range parsers {
		t.Run(parser.name, func(t *testing.T) {
			_, err := parser.parse(largeRSAKeyCert)
			require.ErrorIs(t, err, ErrCertificateTooLarge)
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
	tlsCert, err := NewTLSCert()
	require.NoError(f, err)

	f.Add(tlsCert.Leaf.Raw)
	f.Add(largeRSAKeyCert)
	f.Fuzz(func(t *testing.T, certBytes []byte) {
		require := require.New(t)

		// Verify that any certificate that can be parsed by ParseCertificate
		// can also be parsed by ParseCertificatePermissive.
		{
			strictCert, err := ParseCertificate(certBytes)
			if err == nil {
				permissiveCert, err := ParseCertificatePermissive(certBytes)
				require.NoError(err)
				require.Equal(strictCert, permissiveCert)
			}
		}

		// Verify that any certificate that can't be parsed by
		// ParseCertificatePermissive also can't be parsed by ParseCertificate.
		{
			cert, err := ParseCertificatePermissive(certBytes)
			if err == nil {
				require.NoError(ValidateCertificate(cert))
			} else {
				_, err = ParseCertificate(certBytes)
				require.Error(err) //nolint:forbidigo
			}
		}
	})
}
