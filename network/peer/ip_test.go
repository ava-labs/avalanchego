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
	"github.com/stretchr/testify/require"
)

func TestSignIP(t *testing.T) {
	t.Parallel()
	require := require.New(t)

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
	for i, tv := range tt {
		t.Run(
			fmt.Sprintf("[%d] checking with %q", i, tv.desc),
			func(t *testing.T) {
				tlsCert, err := staking.LoadTLSCertFromFiles(tv.keyPath, tv.certPath)
				require.NoError(err)

				signer, ok := tlsCert.PrivateKey.(crypto.Signer)
				require.Truef(ok, "expected PrivateKey to be crypto.Signer, got %T", tlsCert.PrivateKey)

				hashFunc, sigAlgo, err := deriveHash(tlsCert.Leaf.SignatureAlgorithm)
				require.NoError(err)

				unsignedIP := UnsignedIP{
					IPPort: ips.IPPort{
						IP:   net.IPv6zero,
						Port: 0,
					},
					Timestamp: 0,
				}
				signedIP, err := unsignedIP.Sign(signer, hashFunc, sigAlgo)
				require.NoError(err)

				require.NoError(signedIP.Verify(tlsCert.Leaf))
			},
		)
	}
}
