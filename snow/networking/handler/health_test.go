// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"

	commontracker "github.com/ava-labs/avalanchego/snow/engine/common/tracker"
)

func TestHealthCheckSubnet(t *testing.T) {
	tests := map[string]struct {
		consensusParams snowball.Parameters
	}{
		"default consensus params": {
			consensusParams: snowball.DefaultParameters,
		},
		"custom consensus params": {
			func() snowball.Parameters {
				params := snowball.DefaultParameters
				params.K = params.Alpha
				return params
			}(),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			ctx := snow.DefaultConsensusContextTest()

			vdrs := validators.NewSet()

			resourceTracker, err := tracker.NewResourceTracker(
				prometheus.NewRegistry(),
				resource.NoUsage,
				meter.ContinuousFactory{},
				time.Second,
			)
			require.NoError(err)

			peerTracker := commontracker.NewPeers()
			vdrs.RegisterCallbackListener(peerTracker)

			sb := subnets.New(
				ctx.NodeID,
				subnets.Config{
					ConsensusParameters: test.consensusParams,
				},
			)
			handlerIntf, err := New(
				ctx,
				vdrs,
				nil,
				time.Second,
				testThreadPoolSize,
				resourceTracker,
				validators.UnhandledSubnetConnector,
				sb,
				peerTracker,
			)
			require.NoError(err)

			bootstrapper := &common.BootstrapperTest{
				BootstrapableTest: common.BootstrapableTest{
					T: t,
				},
				EngineTest: common.EngineTest{
					T: t,
				},
			}
			bootstrapper.Default(false)

			engine := &common.EngineTest{T: t}
			engine.Default(false)
			engine.ContextF = func() *snow.ConsensusContext {
				return ctx
			}

			handlerIntf.SetEngineManager(&EngineManager{
				Snowman: &Engine{
					Bootstrapper: bootstrapper,
					Consensus:    engine,
				},
			})

			ctx.State.Set(snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp, // assumed bootstrap is done
			})

			bootstrapper.StartF = func(context.Context, uint32) error {
				return nil
			}

			handlerIntf.Start(context.Background(), false)

			testVdrCount := 4
			vdrIDs := set.NewSet[ids.NodeID](testVdrCount)
			for i := 0; i < testVdrCount; i++ {
				vdrID := ids.GenerateTestNodeID()
				vdrIDs.Add(vdrID)

				require.NoError(vdrs.Add(vdrID, nil, ids.Empty, 100))
			}

			for index, vdr := range vdrs.List() {
				require.NoError(peerTracker.Connected(context.Background(), vdr.NodeID, nil))

				details, err := handlerIntf.HealthCheck(context.Background())
				expectedPercentConnected := float64(index+1) / float64(testVdrCount)
				conf := sb.Config()
				minPercentConnected := conf.ConsensusParameters.MinPercentConnectedHealthy()
				if expectedPercentConnected >= minPercentConnected {
					require.NoError(err)
					continue
				}
				require.ErrorIs(err, ErrNotConnectedEnoughStake)

				detailsMap, ok := details.(map[string]interface{})
				require.True(ok)
				networkingMap, ok := detailsMap["networking"]
				require.True(ok)
				networkingDetails, ok := networkingMap.(map[string]float64)
				require.True(ok)
				percentConnected, ok := networkingDetails["percentConnected"]
				require.True(ok)
				require.Equal(expectedPercentConnected, percentConnected)
			}
		})
	}
}
