// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/json"
	"log"
	"net/netip"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"

	p2pmessage "github.com/ava-labs/avalanchego/message"
	warpmessage "github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
)

var registerSubnetValidatorJSON = []byte(`{
        "subnetID": "2DeHa7Qb6sufPkmQcFWG2uCd4pBPv9WB6dkzroiMQhd1NSRtof",
        "nodeID": "NodeID-9Nm8NNALws11M5J89rXgc46u6L52e7fmz",
        "weight": 49463,
        "blsPublicKey": [
                138,
                25,
                41,
                204,
                242,
                137,
                56,
                208,
                15,
                227,
                216,
                136,
                137,
                144,
                59,
                120,
                205,
                181,
                230,
                232,
                225,
                24,
                80,
                117,
                225,
                16,
                102,
                93,
                83,
                184,
                245,
                240,
                232,
                97,
                238,
                49,
                217,
                4,
                131,
                121,
                19,
                213,
                202,
                233,
                38,
                19,
                70,
                58
        ],
        "expiry": 1727548306
}`)

func main() {
	uri := primary.LocalAPIURI
	infoClient := info.NewClient(uri)
	networkID, err := infoClient.GetNetworkID(context.Background())
	if err != nil {
		log.Fatalf("failed to fetch network ID: %s\n", err)
	}

	var registerSubnetValidator warpmessage.RegisterSubnetValidator
	err = json.Unmarshal(registerSubnetValidatorJSON, &registerSubnetValidator)
	if err != nil {
		log.Fatalf("failed to unmarshal RegisterSubnetValidator message: %s\n", err)
	}
	err = warpmessage.Initialize(&registerSubnetValidator)
	if err != nil {
		log.Fatalf("failed to initialize RegisterSubnetValidator message: %s\n", err)
	}

	var validationID ids.ID = hashing.ComputeHash256Array(registerSubnetValidator.Bytes())
	subnetValidatorRegistration, err := warpmessage.NewSubnetValidatorRegistration(
		validationID,
		true,
	)
	if err != nil {
		log.Fatalf("failed to create SubnetValidatorRegistration message: %s\n", err)
	}

	addressedCall, err := payload.NewAddressedCall(
		nil,
		subnetValidatorRegistration.Bytes(),
	)
	if err != nil {
		log.Fatalf("failed to create AddressedCall message: %s\n", err)
	}

	unsignedWarp, err := warp.NewUnsignedMessage(
		networkID,
		constants.PlatformChainID,
		addressedCall.Bytes(),
	)
	if err != nil {
		log.Fatalf("failed to create unsigned Warp message: %s\n", err)
	}

	p, err := peer.StartTestPeer(
		context.Background(),
		netip.AddrPortFrom(
			netip.AddrFrom4([4]byte{127, 0, 0, 1}),
			9651,
		),
		networkID,
		router.InboundHandlerFunc(func(_ context.Context, msg p2pmessage.InboundMessage) {
			log.Printf("received %s: %s", msg.Op(), msg.Message())
		}),
	)
	if err != nil {
		log.Fatalf("failed to start peer: %s\n", err)
	}

	mesageBuilder, err := p2pmessage.NewCreator(
		logging.NoLog{},
		prometheus.NewRegistry(),
		compression.TypeZstd,
		time.Hour,
	)
	if err != nil {
		log.Fatalf("failed to create message builder: %s\n", err)
	}

	appRequestPayload, err := proto.Marshal(&sdk.SignatureRequest{
		Message:       unsignedWarp.Bytes(),
		Justification: registerSubnetValidator.Bytes(),
	})
	if err != nil {
		log.Fatalf("failed to marshal SignatureRequest: %s\n", err)
	}

	appRequest, err := mesageBuilder.AppRequest(
		constants.PlatformChainID,
		0,
		time.Hour,
		p2p.PrefixMessage(
			p2p.ProtocolPrefix(p2p.SignatureRequestHandlerID),
			appRequestPayload,
		),
	)
	if err != nil {
		log.Fatalf("failed to create AppRequest: %s\n", err)
	}

	p.Send(context.Background(), appRequest)

	time.Sleep(5 * time.Second)

	p.StartClose()
	err = p.AwaitClosed(context.Background())
	if err != nil {
		log.Fatalf("failed to close peer: %s\n", err)
	}
}
