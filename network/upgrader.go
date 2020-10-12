// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto/tls"
	"errors"
	"net"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var (
	errNoCert = errors.New("tls handshake finished with no peer certificate")
)

// Upgrader ...
type Upgrader interface {
	// Must be thread safe
	Upgrade(net.Conn) (ids.ShortID, net.Conn, error)
}

type ipUpgrader struct{}

// NewIPUpgrader ...
func NewIPUpgrader() Upgrader { return ipUpgrader{} }

func (ipUpgrader) Upgrade(conn net.Conn) (ids.ShortID, net.Conn, error) {
	addr := conn.RemoteAddr()
	str := addr.String()
	id := ids.NewShortID(hashing.ComputeHash160Array([]byte(str)))
	return id, conn, nil
}

type tlsServerUpgrader struct {
	config *tls.Config
}

// NewTLSServerUpgrader ...
func NewTLSServerUpgrader(config *tls.Config) Upgrader {
	return tlsServerUpgrader{
		config: config,
	}
}

func (t tlsServerUpgrader) Upgrade(conn net.Conn) (ids.ShortID, net.Conn, error) {
	encConn := tls.Server(conn, t.config)
	if err := encConn.Handshake(); err != nil {
		return ids.ShortID{}, nil, err
	}

	connState := encConn.ConnectionState()
	if len(connState.PeerCertificates) == 0 {
		return ids.ShortID{}, nil, errNoCert
	}
	peerCert := connState.PeerCertificates[0]
	id := ids.NewShortID(
		hashing.ComputeHash160Array(
			hashing.ComputeHash256(peerCert.Raw)))
	return id, encConn, nil
}

type tlsClientUpgrader struct {
	config *tls.Config
}

// NewTLSClientUpgrader ...
func NewTLSClientUpgrader(config *tls.Config) Upgrader {
	return tlsClientUpgrader{
		config: config,
	}
}

func (t tlsClientUpgrader) Upgrade(conn net.Conn) (ids.ShortID, net.Conn, error) {
	encConn := tls.Client(conn, t.config)
	if err := encConn.Handshake(); err != nil {
		return ids.ShortID{}, nil, err
	}

	connState := encConn.ConnectionState()
	if len(connState.PeerCertificates) == 0 {
		return ids.ShortID{}, nil, errNoCert
	}
	peerCert := connState.PeerCertificates[0]
	id := ids.NewShortID(
		hashing.ComputeHash160Array(
			hashing.ComputeHash256(peerCert.Raw)))
	return id, encConn, nil
}
