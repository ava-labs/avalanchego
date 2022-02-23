// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"crypto"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func TestPeer_Close(t *testing.T) {
	initCerts(t)

	ip := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		0,
	)
	id := ids.ShortID(hashing.ComputeHash160Array([]byte(ip.IP().String())))

	listener := &testListener{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 0,
		},
		inbound: make(chan net.Conn, 1<<10),
		closed:  make(chan struct{}),
	}
	caller := &testDialer{
		addr: &net.TCPAddr{
			IP:   net.IPv6loopback,
			Port: 0,
		},
		outbounds: make(map[string]*testListener),
	}

	vdrs := getDefaultManager()
	beacons := validators.NewSet()
	metrics := prometheus.NewRegistry()
	msgCreator, err := message.NewCreator(metrics, true, "dummyNamespace", 10*time.Second)
	assert.NoError(t, err)
	handler := &testHandler{}

	netwrk, err := newTestNetwork(
		id,
		ip,
		defaultVersionManager,
		vdrs,
		beacons,
		cert0.PrivateKey.(crypto.Signer),
		ids.Set{},
		tlsConfig0,
		listener,
		caller,
		metrics,
		msgCreator,
		handler,
	)
	assert.NoError(t, err)
	assert.NotNil(t, netwrk)

	ip1 := utils.NewDynamicIPDesc(
		net.IPv6loopback,
		1,
	)
	caller.outbounds[ip1.IP().String()] = listener
	conn, err := caller.Dial(context.Background(), ip1.IP())
	assert.NoError(t, err)

	basenetwork := netwrk.(*network)

	newmsgbytes := []byte("hello")

	// fake a peer, and write a message
	peer := newPeer(basenetwork, conn, ip1.IP())
	peer.sendQueue = make([]message.OutboundMessage, 0)
	testMsg := message.NewTestMsg(message.GetVersion, newmsgbytes, false)
	peer.Send(testMsg)

	go func() {
		err := netwrk.Close()
		assert.NoError(t, err)
	}()

	peer.Close()
}
