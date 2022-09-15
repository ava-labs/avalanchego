// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"time"

	"github.com/ava-labs/avalanchego/message"
	test_peer "github.com/ava-labs/avalanchego/network/peer/test"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
)

var (
	targetIP    string
	targetPort  uint
	networkID   uint
	stakingKey  string
	stakingCert string
	useProto    bool
)

func main() {
	flag.StringVar(&targetIP, "target-ip", net.IPv6loopback.String(), "ip of the target peer")
	flag.UintVar(&targetPort, "target-port", 9651, "port of the target peer")
	flag.UintVar(&networkID, "network-id", uint(constants.LocalID), "network ID")
	flag.StringVar(&stakingKey, "staking-key", "", "staking key for this test peer")
	flag.StringVar(&stakingCert, "staking-cert", "", "staking cert for this test peer")
	flag.BoolVar(&useProto, "use-proto", false, "'true' to use protobufs message creator")
	flag.Parse()

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	peerIP := ips.IPPort{
		IP:   net.ParseIP(targetIP),
		Port: uint16(targetPort),
	}
	fmt.Println("target ip:port", peerIP.String())

	opts := []test_peer.OpFunc{test_peer.WithNetworkID(uint32(networkID)), test_peer.WithProtoMessage(useProto)}
	if stakingKey != "" && stakingCert != "" {
		opts = append(opts, test_peer.WithStakingCert(stakingKey, stakingCert))
	}

	peer, err := test_peer.Start(
		ctx,
		peerIP,
		router.InboundHandlerFunc(func(msg message.InboundMessage) {
			fmt.Printf("handling %s\n", msg.Op())
		}),
		opts...,
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
