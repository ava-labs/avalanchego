// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/snow/consensus/simplex"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/example/xsvm"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
)

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

func newTestSubnets(t *testing.T, subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey) *Subnets {
	config := map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		subnetID: {
			SimplexParameters: &simplex.Parameters{
				MaxNetworkDelay:    10 * time.Second,
				MaxRebroadcastWait: 5 * time.Second,
				InitialValidators: []simplex.ValidatorInfo{
					{
						NodeID:    nodeID,
						PublicKey: pk.Compress(),
					},
				},
			},
		},
	}

	subnets, err := NewSubnets(ids.EmptyNodeID, config)
	require.NoError(t, err)
	return subnets
}

func newTestVMManager(t *testing.T, vmID ids.ID) *vms.Manager {
	vmManager := vms.NewManager(logging.NoLog{}, ids.NewAliaser())
	require.NoError(t, vmManager.RegisterFactory(t.Context(), vmID, &xsvm.Factory{}))
	return vmManager
}

func TestCreateSimplexChain(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	genesisBytes, err := genesis.Codec.Marshal(genesis.CodecVersion, &genesis.Genesis{})
	require.NoError(t, err)

	chainParams := ChainParameters{
		ID:          ids.GenerateTestID(),
		SubnetID:    ids.GenerateTestID(),
		VMID:        ids.GenerateTestID(),
		GenesisData: genesisBytes,
	}
	logger := logging.NewLogger("test", logging.NewWrappedCore(logging.Debug, os.Stdout, logging.Plain.ConsoleEncoder()))
	signer, err := localsigner.New()
	require.NoError(t, err)

	subnets := newTestSubnets(t, chainParams.SubnetID, nodeID, signer.PublicKey())

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

	mc, err := message.NewCreator(
		prometheus.NewRegistry(),
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	require.NoError(t, err)

	tm := newTimeoutManager(t)
	router := newRouter(t, tm)
	cfg, err := network.NewTestNetworkConfig(
		prometheus.NewRegistry(),
		constants.LocalID,
		validators,
		set.Set[ids.ID]{},
	)
	require.NoError(t, err)

	net, err := network.NewTestNetwork(logger, prometheus.NewRegistry(), cfg, router)
	require.NoError(t, err)

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
			LogLevel:     logging.Debug,
			DisplayLevel: logging.Debug,
			LoggerName:   "chain_logger",
		}),

		VMManager:  newTestVMManager(t, chainParams.VMID),
		Validators: validators,
		MsgCreator: mc,
		Net:        net,
		// For handler initialization
		ResourceTracker: resourceTracker,

		// For health check
		Health: healthChecker,

		// Database
		DB: memdb.New(),

		// Register the chain with router and timeout manager
		TimeoutManager: tm,
		Router:         router,

		FrontierPollFrequency:   constants.DefaultFrontierPollFrequency,
		ConsensusAppConcurrency: constants.DefaultConsensusAppConcurrency,
	}

	chainManager, err := New(managerConfig)
	require.NoError(t, err)
	defer chainManager.Shutdown()

	// Create the chain synchronously rather than going through the chain
	// creator goroutine, which is gated on the P-chain being bootstrapped.
	chainManager.(*manager).createChain(chainParams)

	primaryAlias := chainManager.PrimaryAliasOrDefault(chainParams.ID)
	id, err := chainManager.Lookup(primaryAlias)
	require.NoError(t, err)
	require.Equal(t, chainParams.ID, id)

	require.Eventually(t, func() bool {
		return chainManager.IsBootstrapped(id)
	}, time.Minute, time.Millisecond*100)
}
