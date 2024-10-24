// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto/rsa"
	"crypto/tls"
	"errors"
	"io"

	"github.com/ava-labs/avalanchego/staking"
)

// TLSConfig returns the TLS config that will allow secure connections to other
// peers.
//
// It is safe, and typically expected, for [keyLogWriter] to be [nil].
// [keyLogWriter] should only be enabled for debugging.
func TLSConfig(cert tls.Certificate, keyLogWriter io.Writer) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
		// We do not use the TLS CA functionality to authenticate a
		// hostname. We only require an authenticated channel based on the
		// peer's public key. Therefore, we can safely skip CA verification.
		//
		// During our security audit by Quantstamp, this was investigated
		// and confirmed to be safe and correct.
		InsecureSkipVerify: true, //#nosec G402
		MinVersion:         tls.VersionTLS13,
		KeyLogWriter:       keyLogWriter,
		VerifyConnection:   ValidateRSACertificate,
	}
}

// ValidateRSACertificate validates TLS certificates
// with RSA public keys in the leaf of the certificate chain of the given connection state.
func ValidateRSACertificate(cs tls.ConnectionState) error {
	if len(cs.PeerCertificates) == 0 {
		return errors.New("no certificates sent by peer")
	}

	if cs.PeerCertificates[0] == nil {
		return errors.New("certificate sent by peer is empty")
	}

	pk := cs.PeerCertificates[0].PublicKey
	if pk == nil {
		return errors.New("no public key sent by peer")
	}

	switch rsaKey := pk.(type) {
	case *rsa.PublicKey:
		return staking.ValidateRSAPublicKeyIsWellFormed(rsaKey)
	default:
		return nil
	}
}
