// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/ips"
)

func TestSignedIpVerify(t *testing.T) {
	tlsCert1, err := staking.NewTLSCert()
	require.NoError(t, err)
	cert1, err := staking.ParseCertificate(tlsCert1.Leaf.Raw)
	require.NoError(t, err)
	tlsKey1 := tlsCert1.PrivateKey.(crypto.Signer)
	blsKey1, err := bls.NewSecretKey()
	require.NoError(t, err)

	tlsCert2, err := staking.NewTLSCert()
	require.NoError(t, err)
	cert2, err := staking.ParseCertificate(tlsCert2.Leaf.Raw)
	require.NoError(t, err)

	now := time.Now()

	type test struct {
		name         string
		tlsSigner    crypto.Signer
		blsSigner    *bls.SecretKey
		expectedCert *staking.Certificate
		ip           UnsignedIP
		maxTimestamp time.Time
		expectedErr  error
	}

	tests := []test{
		{
			name:         "valid (before max time)",
			tlsSigner:    tlsKey1,
			blsSigner:    blsKey1,
			expectedCert: cert1,
			ip: UnsignedIP{
				IPPort: ips.IPPort{
					IP:   net.IPv4(1, 2, 3, 4),
					Port: 1,
				},
				Timestamp: uint64(now.Unix()) - 1,
			},
			maxTimestamp: now,
			expectedErr:  nil,
		},
		{
			name:         "valid (at max time)",
			tlsSigner:    tlsKey1,
			blsSigner:    blsKey1,
			expectedCert: cert1,
			ip: UnsignedIP{
				IPPort: ips.IPPort{
					IP:   net.IPv4(1, 2, 3, 4),
					Port: 1,
				},
				Timestamp: uint64(now.Unix()),
			},
			maxTimestamp: now,
			expectedErr:  nil,
		},
		{
			name:         "timestamp too far ahead",
			tlsSigner:    tlsKey1,
			blsSigner:    blsKey1,
			expectedCert: cert1,
			ip: UnsignedIP{
				IPPort: ips.IPPort{
					IP:   net.IPv4(1, 2, 3, 4),
					Port: 1,
				},
				Timestamp: uint64(now.Unix()) + 1,
			},
			maxTimestamp: now,
			expectedErr:  errTimestampTooFarInFuture,
		},
		{
			name:         "sig from wrong cert",
			tlsSigner:    tlsKey1,
			blsSigner:    blsKey1,
			expectedCert: cert2, // note this isn't cert1
			ip: UnsignedIP{
				IPPort: ips.IPPort{
					IP:   net.IPv4(1, 2, 3, 4),
					Port: 1,
				},
				Timestamp: uint64(now.Unix()),
			},
			maxTimestamp: now,
			expectedErr:  errInvalidTLSSignature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signedIP, err := tt.ip.Sign(tt.tlsSigner, tt.blsSigner)
			require.NoError(t, err)

			err = signedIP.Verify(tt.expectedCert, tt.maxTimestamp)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
