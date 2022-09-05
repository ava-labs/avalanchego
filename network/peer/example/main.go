// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils/ips"
)

func main() {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	peerIP := ips.IPPort{
		IP:   net.ParseIP("54.94.43.49"),
		Port: 9651,
	}
	peer, err := peer.StartTestPeer(
		ctx,
		peerIP,
		1,
		router.InboundHandlerFunc(func(msg message.InboundMessage) {
			fmt.Printf("handling %s\n", msg.Op())
		}),
	)
	if err != nil {
		panic(err)
	}

	// Send messages here with [peer.Send].

	peer.StartClose()
	err = peer.AwaitClosed(ctx)
	if err != nil {
		panic(err)
	}
}
