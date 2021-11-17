// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var (
	errNoCert          = errors.New("tls handshake finished with no peer certificate")
	_         Upgrader = &tlsServerUpgrader{}
	_         Upgrader = &tlsClientUpgrader{}
)

type Upgrader interface {
	// Must be thread safe
	Upgrade(net.Conn) (ids.ShortID, net.Conn, *x509.Certificate, error)
}

type tlsServerUpgrader struct {
	config *tls.Config
}

func NewTLSServerUpgrader(config *tls.Config) Upgrader {
	return tlsServerUpgrader{
		config: config,
	}
}

func (t tlsServerUpgrader) Upgrade(conn net.Conn) (ids.ShortID, net.Conn, *x509.Certificate, error) {
	return connToIDAndCert(tls.Server(conn, t.config))
}

type tlsClientUpgrader struct {
	config *tls.Config
}

func NewTLSClientUpgrader(config *tls.Config) Upgrader {
	return tlsClientUpgrader{
		config: config,
	}
}

func (t tlsClientUpgrader) Upgrade(conn net.Conn) (ids.ShortID, net.Conn, *x509.Certificate, error) {
	return connToIDAndCert(tls.Client(conn, t.config))
}

func connToIDAndCert(conn *tls.Conn) (ids.ShortID, net.Conn, *x509.Certificate, error) {
	if err := conn.Handshake(); err != nil {
		return ids.ShortID{}, nil, nil, err
	}

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return ids.ShortID{}, nil, nil, errNoCert
	}
	peerCert := state.PeerCertificates[0]
	return certToID(peerCert), conn, peerCert, nil
}

func certToID(cert *x509.Certificate) ids.ShortID {
	return ids.ShortID(
		hashing.ComputeHash160Array(
			hashing.ComputeHash256(cert.Raw)))
}
