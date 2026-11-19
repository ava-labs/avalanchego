// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestValidParametersMembership covers the membership CA checks in
// ValidParameters.
func TestValidParametersMembership(t *testing.T) {
	tests := []struct {
		name        string
		s           Config
		expectedErr error
	}{
		{
			name: "membershipCA on public subnet",
			s: Config{
				MembershipCA:   []string{"-----BEGIN CERTIFICATE-----"},
				SnowParameters: &validParameters,
			},
			expectedErr: ErrMembershipCAWhenNotValidatorOnly,
		},
		{
			name: "membershipCAPath and membershipCA",
			s: Config{
				ValidatorOnly:    true,
				MembershipCAPath: "/path/to/membership-ca.pem",
				MembershipCA:     []string{"-----BEGIN CERTIFICATE-----"},
				SnowParameters:   &validParameters,
			},
			expectedErr: ErrTooManyMembershipCASources,
		},
		{
			// LoadMembershipCA parses it, once, when the node starts.
			name: "malformed membershipCA is not read",
			s: Config{
				ValidatorOnly:  true,
				MembershipCA:   []string{"not a PEM block"},
				SnowParameters: &validParameters,
			},
		},
		{
			name: "membershipCRL without membershipCA",
			s: Config{
				ValidatorOnly:  true,
				MembershipCRL:  []string{"-----BEGIN X509 CRL-----"},
				SnowParameters: &validParameters,
			},
			expectedErr: ErrMembershipCRLWithoutMembershipCA,
		},
		{
			name: "membershipCRLPath and membershipCRL",
			s: Config{
				ValidatorOnly:     true,
				MembershipCA:      []string{"-----BEGIN CERTIFICATE-----"},
				MembershipCRLPath: "/path/to/membership-crl.pem",
				MembershipCRL:     []string{"-----BEGIN X509 CRL-----"},
				SnowParameters:    &validParameters,
			},
			expectedErr: ErrTooManyMembershipCRLSources,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.s.ValidParameters()
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
