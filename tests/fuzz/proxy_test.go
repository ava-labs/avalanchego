// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"crypto/rand"
	"crypto/tls"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/units"
)

// TestProxyCarriesTLS sends data across many TLS records, with the node's TLS
// config at both ends. A changed, dropped, or reordered byte would fail the
// TLS session.
func TestProxyCarriesTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = ln.Close() // the test is over
	})

	serverCert, err := staking.NewTLSCert()
	require.NoError(t, err)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		server := tls.Server(conn, peer.TLSConfig(*serverCert, nil))
		defer server.Close()

		_, _ = io.Copy(server, server) // the client checks what comes back
	}()

	p := &proxy{}
	t.Cleanup(p.Close)
	conn, err := p.DialContext(t.Context(), "tcp", ln.Addr().String())
	require.NoError(t, err)

	clientCert, err := staking.NewTLSCert()
	require.NoError(t, err)
	client := tls.Client(conn, peer.TLSConfig(*clientCert, nil))
	defer client.Close()
	require.NoError(t, client.HandshakeContext(t.Context()))

	want := make([]byte, 256*units.KiB)
	_, _ = rand.Read(want) // never fails
	written := make(chan error, 1)
	go func() {
		_, err := client.Write(want)
		written <- err
	}()

	got := make([]byte, len(want))
	_, err = io.ReadFull(client, got)
	require.NoError(t, err)
	require.NoError(t, <-written)
	require.Equal(t, want, got)
}
