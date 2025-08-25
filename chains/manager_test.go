package chains

import (
	"context"
	"os"
	"testing"
	"time"

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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newRouter(t *testing.T) router.Router {
	ctrl := gomock.NewController(t)
	r := routermock.NewRouter(ctrl)
	r.EXPECT().AddChain(gomock.Any(), gomock.Any()).Return().AnyTimes()
	return r
}

func newTimeoutManager(t *testing.T) timeout.Manager {
	require := require.New(t)

	config := &timer.AdaptiveTimeoutConfig{
		InitialTimeout:    2 * time.Second,
		MinimumTimeout:    1 * time.Second,
		MaximumTimeout:    10 * time.Second,
		TimeoutCoefficient: 1.5,
		TimeoutHalflife:   10 * time.Second,
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

func newMockVMManager(t *testing.T, log logging.Logger) vms.Manager {
	ctrl := gomock.NewController(t)
	vm := &blocktest.VM{}
	vm.InitializeF = func(ctx context.Context, chainCtx *snow.Context, db database.Database, genesisBytes []byte, upgradeBytes []byte, configBytes []byte, fxs []*common.Fx, appSender common.AppSender) error {
		log.Info("initializing mock vm")
		return nil
	}

	vm.GetBlockIDAtHeightF = func(_ context.Context, height uint64) (ids.ID, error) {
		return snowmantest.GenesisID, nil
	}
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
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

func TestCreateSimplexChain(t *testing.T) {
	writeCloser := os.Stdout
	logFormat, err := logging.ToFormat("auto", writeCloser.Fd())
	require.NoError(t, err)
	logger := logging.NewLogger("chain_manager_test", logging.NewWrappedCore(logging.Verbo, writeCloser, logFormat.ConsoleEncoder()))

	nodeID := ids.GenerateTestNodeID()
	chainParams := ChainParameters{
		ID:        ids.GenerateTestID(),
		SubnetID:  ids.GenerateTestID(),
		VMID:    ids.GenerateTestID(),
	}
	
	config := map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
		chainParams.SubnetID: {
			ConsensusConfig: subnets.ConsensusConfig{
				SimplexParams: &subnets.SimplexParameters{
					Enabled: true,
				},
			},
		},
	}

	subnets, err := NewSubnets(ids.EmptyNodeID, config)
	require.NoError(t, err)

	signer, err := localsigner.New()
	require.NoError(t, err)

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.DefaultRegisterer,
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(t, err)

	validators := validators.NewManager()
	err = validators.AddStaker(chainParams.SubnetID, nodeID, signer.PublicKey(), ids.GenerateTestID(), 1)
	require.NoError(t, err)

	hChecker, err := health.New(logger, prometheus.DefaultRegisterer)
	require.NoError(t, err)
	managerConfig := &ManagerConfig{
		Metrics: metrics.NewLabelGatherer("chain"),
		MeterDBMetrics: metrics.NewLabelGatherer("dbmetrics"),
		Log: logger,
		Subnets: subnets,
		LogFactory: logging.NewFactory(logging.Config{
			LogLevel: logging.Debug,
			LoggerName: "chain_logger",
		}),
		StakingBLSKey: signer,
		VMManager: newMockVMManager(t, logger),
		Validators: validators,

		NodeID: nodeID,
		// for the handler
		FrontierPollFrequency: 1 * time.Second,
		ConsensusAppConcurrency: 1,
		ResourceTracker: resourceTracker,

		// for health check
		Health: hChecker,

		// post buildchain
		TimeoutManager: newTimeoutManager(t),
		Router: newRouter(t),

	}
	manager, err := New(managerConfig)
	require.NoError(t, err)

	err = manager.StartChainCreatorNoPChain()
	require.NoError(t, err)

	// queue chain creation
	manager.QueueChainCreation(chainParams)

	time.Sleep(1 * time.Second)
	primaryAlias := manager.PrimaryAliasOrDefault(chainParams.ID)
	
	id, err := manager.Lookup(primaryAlias)
	require.NoError(t, err)
	require.Equal(t, chainParams.ID, id)
	// we should get a notification that the chain was created

}