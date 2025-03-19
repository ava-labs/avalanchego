// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

type testExternalHandler struct{}

func (*testExternalHandler) HandleInbound(context.Context, message.InboundMessage) {}
func (*testExternalHandler) Connected(ids.NodeID, *version.Application, ids.ID)    {}
func (*testExternalHandler) Disconnected(ids.NodeID)                               {}

func main() {
	log := logging.NewLogger(
		"networking",
		logging.NewWrappedCore(
			logging.Info,
			os.Stdout,
			logging.Colors.ConsoleEncoder(),
		),
	)

	manager := validators.NewManager()
	subnetIDs := []ids.ID{
		ids.FromStringOrPanic("5moznRzaAEhzWkNTQVdT1U4Kb9EU7dbsKZQNmHwtN5MGVQRyT"),
		constants.PrimaryNetworkID,
	}
	pClient := platformvm.NewClient(primary.MainnetAPIURI)
	for _, subnetID := range subnetIDs {
		vdrs, err := pClient.GetCurrentValidators(context.Background(), subnetID, nil)
		if err != nil {
			log.Fatal("failed to create test network config",
				zap.Error(err),
			)
			return
		}

		for _, vdr := range vdrs {
			var pk *bls.PublicKey
			if len(vdr.PublicKey) > 0 {
				pk, err = bls.PublicKeyFromCompressedBytes(vdr.PublicKey)
				if err != nil {
					log.Fatal("failed to create public key from bytes",
						zap.Error(err),
					)
					return
				}
			}
			if vdr.Signer != nil {
				pk = vdr.Signer.Key()
			}

			txID := vdr.TxID
			if vdr.ValidationID != ids.Empty {
				txID = vdr.ValidationID
			}

			err = manager.AddStaker(
				subnetID,
				vdr.NodeID,
				pk,
				txID,
				uint64(vdr.Weight),
			)
			if err != nil {
				log.Fatal("failed to add staker",
					zap.Error(err),
				)
				return
			}
		}
	}
	log.Info("Successfully added validators to the manager",
		zap.Int("subnet", manager.NumValidators(subnetIDs[0])),
		zap.Int("primary", manager.NumValidators(subnetIDs[1])),
	)

	// If we want to be able to communicate with non-primary network subnets, we
	// should register them here.
	trackedSubnets := set.Of(subnetIDs[0])

	// Messages and connections are handled by the external handler.
	metrics := prometheus.NewRegistry()
	cfg, err := network.NewTestNetworkConfig(
		metrics,
		constants.MainnetID,
		manager,
		trackedSubnets,
	)
	if err != nil {
		log.Fatal(
			"failed to create test network config",
			zap.Error(err),
		)
		return
	}
	network, err := network.NewTestNetwork(
		log,
		metrics,
		cfg,
		&testExternalHandler{},
	)
	if err != nil {
		log.Fatal(
			"failed to create test network",
			zap.Error(err),
		)
		return
	}

	// We need to initially connect to some nodes in the network before peer
	// gossip will enable connecting to all the remaining nodes in the network.
	bootstrappers := genesis.SampleBootstrappers(constants.MainnetID, 5)
	for _, bootstrapper := range bootstrappers {
		network.ManuallyTrack(bootstrapper.ID, bootstrapper.IP)
	}

	for {
		time.Sleep(1 * time.Second)
		log.Info("peer info",
			zap.Int("count", len(network.PeerInfo(nil))),
		)
	}
}
