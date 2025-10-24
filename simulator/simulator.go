// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simulator

import (
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/network"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/ids"
	"time"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/logging"
	"golang.org/x/net/nettest"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"context"
	"golang.org/x/sync/errgroup"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"testing"
)

type Handler interface {

}

type Node struct{
	Handler struct{}
	network network.Network
}

type Simulator struct {
	nodes []Node
	eg *errgroup.Group
}

func NewVMSimulator[T block.ChainVM](t *testing.T, ts ...T) (*Simulator, error) {
	nodes := make([]Node, 0, len(ts))
	for i, vm := range ts {
		metrics := prometheus.NewRegistry()
		validators := validators.NewManager()
		cfg, err := network.NewTestNetworkConfig(metrics,
			constants.LocalID,
			validators,
			set.Set[ids.ID]{},
		)
		if err != nil {
			return nil, fmt.Errorf("initializing network config: %w", err)
		}

		msgCreator, err := message.NewCreator(metrics,
			compression.TypeNone,
			30 * time.Second,
			)
		if err != nil {
			return nil, fmt.Errorf("initializing message creator: %w", err)
		}

		log := logging.NewLogger(fmt.Sprintf("node-%d", i))
		listener, err := nettest.NewLocalListener("tcp")
		if err != nil {
			return nil, fmt.Errorf("initializing listener: %w", err)
		}

		dialer := dialer.NewDialer(
			"tcp",
			dialer.Config{
				ThrottleRps:       1000,
				ConnectionTimeout: 5 * time.Second,
			},
			log,
		)

		router := &router.ChainRouter{}
		snowCtx := snowtest.Context(t, ids.Empty)
		handler := handler.New(snowCtx, )
		router.AddChain(context.TODO(), handler.Handler())

		network, err := network.NewNetwork(
			cfg,
			time.Time{},
			msgCreator,
			metrics,
			log,
			listener,
			dialer,
			router,
		)
		if err != nil {
			return nil, fmt.Errorf("initializing network: %w", err)
		}

		nodes = append(nodes, Node{
			Handler: struct{}{},
			network: network,
		})
	}

	return &Simulator{
		nodes: nodes,
	}, nil
}

func (s *Simulator) Run(ctx context.Context) error {
	for _, node := range s.nodes {
		s.eg.Go(func() error {
			return node.network.Dispatch()
		})
	}

	return s.eg.Wait()
}

func NewConsensusSimulator[T common.Handler](ts ... T) Simulator {

}
