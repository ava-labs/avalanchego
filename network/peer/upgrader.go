// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto/tls"
	"errors"
	"net"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
)

var (
	errNoCert = errors.New("tls handshake finished with no peer certificate")

	_ Upgrader = (*tlsServerUpgrader)(nil)
	_ Upgrader = (*tlsClientUpgrader)(nil)
)

type Upgrader interface {
	// Must be thread safe
	Upgrade(net.Conn) (ids.NodeID, net.Conn, *staking.Certificate, error)
}

type tlsServerUpgrader struct {
	config       *tls.Config
	invalidCerts prometheus.Counter
}

func NewTLSServerUpgrader(config *tls.Config, invalidCerts prometheus.Counter) Upgrader {
	return &tlsServerUpgrader{
		config:       config,
		invalidCerts: invalidCerts,
	}
}

func (t *tlsServerUpgrader) Upgrade(conn net.Conn) (ids.NodeID, net.Conn, *staking.Certificate, error) {
	return connToIDAndCert(tls.Server(conn, t.config), t.invalidCerts)
}

type tlsClientUpgrader struct {
	config       *tls.Config
	invalidCerts prometheus.Counter
}

func NewTLSClientUpgrader(config *tls.Config, invalidCerts prometheus.Counter) Upgrader {
	return &tlsClientUpgrader{
		config:       config,
		invalidCerts: invalidCerts,
	}
}

func (t *tlsClientUpgrader) Upgrade(conn net.Conn) (ids.NodeID, net.Conn, *staking.Certificate, error) {
	return connToIDAndCert(tls.Client(conn, t.config), t.invalidCerts)
}

func connToIDAndCert(conn *tls.Conn, invalidCerts prometheus.Counter) (ids.NodeID, net.Conn, *staking.Certificate, error) {
	if err := conn.Handshake(); err != nil {
		return ids.EmptyNodeID, nil, nil, err
	}

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return ids.EmptyNodeID, nil, nil, errNoCert
	}

	tlsCert := state.PeerCertificates[0]
	peerCert, err := staking.ParseCertificate(tlsCert.Raw)
	if err != nil {
		invalidCerts.Inc()
		return ids.EmptyNodeID, nil, nil, err
	}

	nodeID := ids.NodeIDFromCert(peerCert)
	return nodeID, conn, peerCert, nil
}
