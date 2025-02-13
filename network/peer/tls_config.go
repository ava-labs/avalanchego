// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/tls"
	"errors"
	"io"

	"github.com/ava-labs/avalanchego/staking"
)

var (
	ErrNoCertsSent        = errors.New("no certificates sent by peer")
	ErrEmptyCert          = errors.New("certificate sent by peer is empty")
	ErrEmptyPublicKey     = errors.New("no public key sent by peer")
	ErrCurveMismatch      = errors.New("only P256 is allowed for ECDSA")
	ErrUnsupportedKeyType = errors.New("key type is not supported")
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
		VerifyConnection:   ValidateCertificate,
	}
}

// ValidateCertificate validates TLS certificates according their public keys on the leaf certificate in the certification chain.
func ValidateCertificate(cs tls.ConnectionState) error {
	if len(cs.PeerCertificates) == 0 {
		return ErrNoCertsSent
	}

	if cs.PeerCertificates[0] == nil {
		return ErrEmptyCert
	}

	pk := cs.PeerCertificates[0].PublicKey

	switch key := pk.(type) {
	case *ecdsa.PublicKey:
		if key == nil {
			return ErrEmptyPublicKey
		}
		if key.Curve != elliptic.P256() {
			return ErrCurveMismatch
		}
		return nil
	case *rsa.PublicKey:
		return staking.ValidateRSAPublicKeyIsWellFormed(key)
	default:
		return ErrUnsupportedKeyType
	}
}
