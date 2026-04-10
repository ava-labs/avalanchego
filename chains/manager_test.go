// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/simplex"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/vms"
)

// startChainCreatorNoPChain is used for testing to bypass setting up the pchain
// and unblocking the chain creator
func (m *manager) startChainCreatorNoPChain() {
	m.chainCreatorExited.Add(1)
	go m.dispatchChainCreator()

	// typically creating the pchain would unblock the chain creator channel
	close(m.unblockChainCreatorCh)
}

// newRouter returns a mock router that mocks the call of adding a chain to the router.
func newRouter(t *testing.T, tm *timeout.Manager) router.Router {
	chainRouter := router.ChainRouter{}
	require.NoError(t, chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		tm,
		time.Second,
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
		nil,
		router.HealthConfig{},
		prometheus.NewRegistry(),
	))

	return &chainRouter
}

func newTimeoutManager(t *testing.T) *timeout.Manager {
	require := require.New(t)

	config := &timer.AdaptiveTimeoutConfig{
		InitialTimeout:     2 * time.Second,
		MinimumTimeout:     1 * time.Second,
		MaximumTimeout:     10 * time.Second,
		TimeoutCoefficient: 1.5,
		TimeoutHalflife:    10 * time.Second,
	}
	timeoutManager, err := timeout.NewManager(
		config,
		benchlist.NewNoBenchlist(),
		prometheus.DefaultRegisterer,
		prometheus.DefaultRegisterer,
	)
	require.NoError(err)

	return timeoutManager
}

func newTestSubnets(t *testing.T, subnetID ids.ID) *Subnets {
	config := map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		subnetID: {
			SimplexParameters: &simplex.Parameters{
				MaxNetworkDelay:    10 * time.Second,
				MaxRebroadcastWait: 5 * time.Second,
			},
		},
	}

	subnets, err := NewSubnets(ids.EmptyNodeID, config)
	require.NoError(t, err)
	return subnets
}

func TestCreateSimplexChain(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	chainParams := ChainParameters{
		ID:       ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
		VMID:     ids.GenerateTestID(),
	}
	logger := logging.NoLog{}
	subnets := newTestSubnets(t, chainParams.SubnetID)
	signer, err := localsigner.New()
	require.NoError(t, err)

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.DefaultRegisterer,
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)

	// Set the validators of the simplex chain. It must include our nodeID.
	validators := validators.NewManager()
	require.NoError(t, validators.AddStaker(chainParams.SubnetID, nodeID, signer.PublicKey(), ids.GenerateTestID(), 1))

	healthChecker, err := health.New(logger, prometheus.DefaultRegisterer)
	require.NoError(t, err)

	tm := newTimeoutManager(t)
	router := newRouter(t, tm)
	managerConfig := &ManagerConfig{
		NodeID:        nodeID,
		StakingBLSKey: signer,
		Subnets:       subnets,

		// Metrics
		Metrics:        metrics.NewLabelGatherer("chain"),
		MeterDBMetrics: metrics.NewLabelGatherer("dbmetrics"),

		// Logging
		Log: logger,
		LogFactory: logging.NewFactory(logging.Config{
			LogLevel:   logging.Debug,
			LoggerName: "chain_logger",
		}),

		VMManager:  *vms.NewManager(logging.NoLog{}, ids.NewAliaser()),
		Validators: validators,

		// For handler initialization
		FrontierPollFrequency:   1 * time.Second,
		ConsensusAppConcurrency: 1,
		ResourceTracker:         resourceTracker,

		// For health check
		Health: healthChecker,

		// Register the chain with router and timeout manager
		TimeoutManager: tm,
		Router:         router,
	}

	chainManager, err := New(managerConfig)
	require.NoError(t, err)

	chainManager.(*manager).startChainCreatorNoPChain()

	// Queue chain creation
	chainManager.QueueChainCreation(chainParams)
	primaryAlias := chainManager.PrimaryAliasOrDefault(chainParams.ID)

	for {
		id, err := chainManager.Lookup(primaryAlias)
		if errors.Is(err, ids.ErrNoIDWithAlias) {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, chainParams.ID, id)
		return
	}
}
