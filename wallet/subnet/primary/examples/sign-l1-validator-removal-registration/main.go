// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/proto/pb/platformvm"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"

	p2pmessage "github.com/ava-labs/avalanchego/message"
	warpmessage "github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
)

var registerL1ValidatorJSON = []byte(`{
        "subnetID": "2DeHa7Qb6sufPkmQcFWG2uCd4pBPv9WB6dkzroiMQhd1NSRtof",
        "nodeID": "0x550f3c8f2ebd89e6a69adca196bea38a1b4d65bc",
        "blsPublicKey": [
                178,
                119,
                51,
                152,
                247,
                239,
                52,
                16,
                89,
                246,
                6,
                11,
                76,
                81,
                114,
                139,
                141,
                251,
                127,
                202,
                205,
                177,
                62,
                75,
                152,
                207,
                170,
                120,
                86,
                213,
                226,
                226,
                104,
                135,
                245,
                231,
                226,
                223,
                64,
                19,
                242,
                246,
                227,
                12,
                223,
                23,
                193,
                219
        ],
        "expiry": 1728331617,
        "remainingBalanceOwner": {
                "threshold": 0,
                "addresses": null
        },
        "disableOwner": {
                "threshold": 0,
                "addresses": null
        },
        "weight": 1
}`)

func main() {
	uri := primary.LocalAPIURI
	infoClient := info.NewClient(uri)
	networkID, err := infoClient.GetNetworkID(context.Background())
	if err != nil {
		log.Fatalf("failed to fetch network ID: %s\n", err)
	}

	var registerL1Validator warpmessage.RegisterL1Validator
	err = json.Unmarshal(registerL1ValidatorJSON, &registerL1Validator)
	if err != nil {
		log.Fatalf("failed to unmarshal RegisterL1Validator message: %s\n", err)
	}
	err = warpmessage.Initialize(&registerL1Validator)
	if err != nil {
		log.Fatalf("failed to initialize RegisterL1Validator message: %s\n", err)
	}

	validationID := registerL1Validator.ValidationID()
	l1ValidatorRegistration, err := warpmessage.NewL1ValidatorRegistration(
		validationID,
		false,
	)
	if err != nil {
		log.Fatalf("failed to create L1ValidatorRegistration message: %s\n", err)
	}

	addressedCall, err := payload.NewAddressedCall(
		nil,
		l1ValidatorRegistration.Bytes(),
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

	justification := platformvm.L1ValidatorRegistrationJustification{
		Preimage: &platformvm.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage{
			RegisterL1ValidatorMessage: registerL1Validator.Bytes(),
		},
	}
	justificationBytes, err := proto.Marshal(&justification)
	if err != nil {
		log.Fatalf("failed to create justification: %s\n", err)
	}

	p, err := peer.StartTestPeer(
		context.Background(),
		netip.AddrPortFrom(
			netip.AddrFrom4([4]byte{127, 0, 0, 1}),
			9651,
		),
		networkID,
		router.InboundHandlerFunc(func(_ context.Context, msg *p2pmessage.InboundMessage) {
			log.Printf("received %s: %s", msg.Op, msg.Message)
		}),
	)
	if err != nil {
		log.Fatalf("failed to start peer: %s\n", err)
	}

	mesageBuilder, err := p2pmessage.NewCreator(
		prometheus.NewRegistry(),
		compression.TypeZstd,
		time.Hour,
	)
	if err != nil {
		log.Fatalf("failed to create message builder: %s\n", err)
	}

	appRequestPayload, err := proto.Marshal(&sdk.SignatureRequest{
		Message:       unsignedWarp.Bytes(),
		Justification: justificationBytes,
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
