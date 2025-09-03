// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/router/routermock"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/vmsmock"
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
func newRouter(t *testing.T) router.Router {
	ctrl := gomock.NewController(t)
	r := routermock.NewRouter(ctrl)
	r.EXPECT().AddChain(gomock.Any(), gomock.Any()).Return().AnyTimes()
	return r
}

func newTimeoutManager(t *testing.T) timeout.Manager {
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
		benchlist.NewManager(&benchlist.Config{}),
		prometheus.DefaultRegisterer,
		prometheus.DefaultRegisterer,
	)
	require.NoError(err)

	return timeoutManager
}

// newMockVMManager returns a VM manager that always returns a mock VM with
// only the genesis block defined.
func newMockVMManager(t *testing.T) vms.Manager {
	ctrl := gomock.NewController(t)
	vm := &blocktest.VM{}
	vm.InitializeF = func(_ context.Context, _ *snow.Context, _ database.Database, _ []byte, _ []byte, _ []byte, _ []*common.Fx, _ common.AppSender) error {
		return nil
	}

	vm.GetBlockIDAtHeightF = func(_ context.Context, height uint64) (ids.ID, error) {
		require.Zero(t, height)
		return snowmantest.GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		require.Equal(t, snowmantest.GenesisID, blkID)
		return snowmantest.Genesis, nil
	}
	vm.LastAcceptedF = func(_ context.Context) (ids.ID, error) {
		return snowmantest.GenesisID, nil
	}

	factory := vmsmock.NewFactory(ctrl)
	factory.EXPECT().New(gomock.Any()).Return(vm, nil).AnyTimes()

	manager := vmsmock.NewManager(ctrl)
	manager.EXPECT().GetFactory(gomock.Any()).Return(factory, nil).AnyTimes()

	return manager
}

func testLogger(t *testing.T) logging.Logger {
	writeCloser := os.Stdout
	logFormat, err := logging.ToFormat("auto", writeCloser.Fd())
	require.NoError(t, err)
	return logging.NewLogger("chain_manager_test", logging.NewWrappedCore(logging.Verbo, writeCloser, logFormat.ConsoleEncoder()))
}

func newTestSubnets(t *testing.T, subnetID ids.ID) *Subnets {
	config := map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		subnetID: {
			ConsensusConfig: subnets.ConsensusConfig{
				SimplexParams: &subnets.SimplexParameters{
					Enabled: true,
				},
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
	logger := testLogger(t)
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

	// set the validators of the simplex chain
	// it must include our nodeID
	validators := validators.NewManager()
	require.NoError(t, validators.AddStaker(chainParams.SubnetID, nodeID, signer.PublicKey(), ids.GenerateTestID(), 1))

	healthChecker, err := health.New(logger, prometheus.DefaultRegisterer)
	require.NoError(t, err)

	router := newRouter(t)
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

		VMManager:  newMockVMManager(t),
		Validators: validators,

		// For handler initialization
		FrontierPollFrequency:   1 * time.Second,
		ConsensusAppConcurrency: 1,
		ResourceTracker:         resourceTracker,

		// For health check
		Health: healthChecker,

		// Register the chain with router and timeout manager
		TimeoutManager: newTimeoutManager(t),
		Router:         router,
	}

	chainManager, err := New(managerConfig)
	require.NoError(t, err)

	chainManager.(*manager).startChainCreatorNoPChain()

	// queue chain creation
	chainManager.QueueChainCreation(chainParams)
	primaryAlias := chainManager.PrimaryAliasOrDefault(chainParams.ID)

	timeout := time.After(10 * time.Second)
	for {
		select {
		case <-timeout:
			require.Fail(t, "timed out waiting for chain to be created")
		default:
		}
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
