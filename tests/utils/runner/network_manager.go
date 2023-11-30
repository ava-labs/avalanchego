// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runner

import (
	"context"
	"fmt"
	"os"
	"time"

	runner_sdk "github.com/ava-labs/avalanche-network-runner/client"
	"github.com/ava-labs/avalanche-network-runner/rpcpb"
	runner_server "github.com/ava-labs/avalanche-network-runner/server"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/subnet-evm/plugin/evm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

// Subnet provides the basic details of a created subnet
// Note: currently assumes one blockchain per subnet
type Subnet struct {
	// SubnetID is the txID of the transaction that created the subnet
	SubnetID ids.ID `json:"subnetID"`
	// Current ANR assumes one blockchain per subnet, so we have a single blockchainID here
	BlockchainID ids.ID `json:"blockchainID"`
	// ValidatorURIs is the base URIs for each participant of the Subnet
	ValidatorURIs []string `json:"validatorURIs"`
}

type ANRConfig struct {
	LogLevel            string
	AvalancheGoExecPath string
	PluginDir           string
	GlobalNodeConfig    string
	GlobalCChainConfig  string
}

// NetworkManager is a wrapper around the ANR to simplify the setup and teardown code
// of tests that rely on the ANR.
type NetworkManager struct {
	ANRConfig ANRConfig

	subnets []*Subnet

	logFactory      logging.Factory
	anrClient       runner_sdk.Client
	anrServer       runner_server.Server
	done            chan struct{}
	serverCtxCancel context.CancelFunc
}

// NewDefaultANRConfig returns a default config for launching the avalanche-network-runner manager
// with both a server and client.
// By default, it expands $GOPATH/src/github.com/ava-labs/avalanchego/build/ directory to extract
// the AvalancheGoExecPath and PluginDir arguments.
// If the AVALANCHEGO_BUILD_PATH environment variable is set, it overrides the default location for
// the AvalancheGoExecPath and PluginDir arguments.
func NewDefaultANRConfig() ANRConfig {
	defaultConfig := ANRConfig{
		LogLevel:            "info",
		AvalancheGoExecPath: os.ExpandEnv("$GOPATH/src/github.com/ava-labs/avalanchego/build/avalanchego"),
		PluginDir:           os.ExpandEnv("$GOPATH/src/github.com/ava-labs/avalanchego/build/plugins"),
		GlobalNodeConfig: `{
			"log-display-level":"info",
			"proposervm-use-current-height":true
		}`,
		GlobalCChainConfig: `{
			"warp-api-enabled": true,
			"log-level": "debug"
		}`,
	}
	// If AVALANCHEGO_BUILD_PATH is populated, override location set by GOPATH
	if envBuildPath, exists := os.LookupEnv("AVALANCHEGO_BUILD_PATH"); exists {
		defaultConfig.AvalancheGoExecPath = fmt.Sprintf("%s/avalanchego", envBuildPath)
		defaultConfig.PluginDir = fmt.Sprintf("%s/plugins", envBuildPath)
	}
	return defaultConfig
}

// NewNetworkManager constructs a new instance of a network manager
func NewNetworkManager(config ANRConfig) *NetworkManager {
	manager := &NetworkManager{
		ANRConfig: config,
	}

	logLevel, err := logging.ToLevel(config.LogLevel)
	if err != nil {
		panic(fmt.Errorf("invalid ANR log level: %w", err))
	}
	manager.logFactory = logging.NewFactory(logging.Config{
		DisplayLevel: logLevel,
		LogLevel:     logLevel,
	})

	return manager
}

// startServer starts a new ANR server and sets/overwrites the anrServer, done channel, and serverCtxCancel function.
func (n *NetworkManager) startServer(ctx context.Context) (<-chan struct{}, error) {
	done := make(chan struct{})
	zapServerLog, err := n.logFactory.Make("server")
	if err != nil {
		return nil, fmt.Errorf("failed to make server log: %w", err)
	}

	logLevel, err := logging.ToLevel(n.ANRConfig.LogLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ANR log level: %w", err)
	}

	n.anrServer, err = runner_server.New(
		runner_server.Config{
			Port:                ":12352",
			GwPort:              ":12353",
			GwDisabled:          false,
			DialTimeout:         10 * time.Second,
			RedirectNodesOutput: true,
			SnapshotsDir:        "",
			LogLevel:            logLevel,
		},
		zapServerLog,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start ANR server: %w", err)
	}
	n.done = done

	// Use a separate background context here, since the server should only be canceled by explicit shutdown
	serverCtx, serverCtxCancel := context.WithCancel(context.Background())
	n.serverCtxCancel = serverCtxCancel
	go func() {
		if err := n.anrServer.Run(serverCtx); err != nil {
			log.Error("Error shutting down ANR server", "err", err)
		} else {
			log.Info("Terminating ANR Server")
		}
		close(done)
	}()

	return done, nil
}

// startClient starts an ANR Client dialing the ANR server at the expected endpoint.
// Note: will overwrite client if it already exists.
func (n *NetworkManager) startClient() error {
	logLevel, err := logging.ToLevel(n.ANRConfig.LogLevel)
	if err != nil {
		return fmt.Errorf("failed to parse ANR log level: %w", err)
	}
	logFactory := logging.NewFactory(logging.Config{
		DisplayLevel: logLevel,
		LogLevel:     logLevel,
	})
	zapLog, err := logFactory.Make("main")
	if err != nil {
		return fmt.Errorf("failed to make client log: %w", err)
	}

	n.anrClient, err = runner_sdk.New(runner_sdk.Config{
		Endpoint:    "0.0.0.0:12352",
		DialTimeout: 10 * time.Second,
	}, zapLog)
	if err != nil {
		return fmt.Errorf("failed to start ANR client: %w", err)
	}

	return nil
}

// initServer starts the ANR server if it is not populated
func (n *NetworkManager) initServer() error {
	if n.anrServer != nil {
		return nil
	}

	_, err := n.startServer(context.Background())
	return err
}

// initClient starts an ANR client if it not populated
func (n *NetworkManager) initClient() error {
	if n.anrClient != nil {
		return nil
	}

	return n.startClient()
}

// init starts the ANR server and client if they are not yet populated
func (n *NetworkManager) init() error {
	if err := n.initServer(); err != nil {
		return err
	}
	return n.initClient()
}

// StartDefaultNetwork constructs a default 5 node network.
func (n *NetworkManager) StartDefaultNetwork(ctx context.Context) (<-chan struct{}, error) {
	if err := n.init(); err != nil {
		return nil, err
	}

	log.Info("Sending 'start'", "AvalancheGoExecPath", n.ANRConfig.AvalancheGoExecPath)

	// Start cluster
	opts := []runner_sdk.OpOption{
		runner_sdk.WithPluginDir(n.ANRConfig.PluginDir),
		runner_sdk.WithGlobalNodeConfig(n.ANRConfig.GlobalNodeConfig),
	}
	if len(n.ANRConfig.GlobalCChainConfig) != 0 {
		opts = append(opts, runner_sdk.WithChainConfigs(map[string]string{
			"C": n.ANRConfig.GlobalCChainConfig,
		}))
	}
	resp, err := n.anrClient.Start(
		ctx,
		n.ANRConfig.AvalancheGoExecPath,
		opts...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start ANR network: %w", err)
	}
	log.Info("successfully started cluster", "RootDataDir", resp.ClusterInfo.RootDataDir, "Subnets", resp.GetClusterInfo().GetSubnets())
	return n.done, nil
}

// SetupNetwork constructs blockchains with the given [blockchainSpecs] and adds them to the network manager.
// Uses [execPath] as the AvalancheGo binary execution path for any started nodes.
// Note: this assumes that the default network has already been constructed.
func (n *NetworkManager) SetupNetwork(ctx context.Context, execPath string, blockchainSpecs []*rpcpb.BlockchainSpec) error {
	// timeout according to how many blockchains we're creating
	timeout := 2 * time.Minute * time.Duration(len(blockchainSpecs))
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := n.init(); err != nil {
		return err
	}
	sresp, err := n.anrClient.CreateBlockchains(
		ctx,
		blockchainSpecs,
	)
	if err != nil {
		return fmt.Errorf("failed to create blockchains: %w", err)
	}

	// TODO: network runner health should imply custom VM healthiness
	// or provide a separate API for custom VM healthiness
	// "start" is async, so wait some time for cluster health
	log.Info("waiting for all VMs to report healthy", "VMID", evm.ID)
	for {
		v, err := n.anrClient.Health(ctx)
		log.Info("Pinged CLI Health", "result", v, "err", err)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		} else if ctx.Err() != nil {
			return fmt.Errorf("failed to await healthy network: %w", ctx.Err())
		}
		break
	}

	status, err := n.anrClient.Status(cctx)
	if err != nil {
		return fmt.Errorf("failed to get ANR status: %w", err)
	}
	nodeInfos := status.GetClusterInfo().GetNodeInfos()

	for i, chainSpec := range blockchainSpecs {
		blockchainIDStr := sresp.ChainIds[i]
		blockchainID, err := ids.FromString(blockchainIDStr)
		if err != nil {
			panic(err)
		}
		subnetIDStr := sresp.ClusterInfo.CustomChains[blockchainIDStr].SubnetId
		subnetID, err := ids.FromString(subnetIDStr)
		if err != nil {
			panic(err)
		}
		subnet := &Subnet{
			SubnetID:     subnetID,
			BlockchainID: blockchainID,
		}
		for _, nodeName := range chainSpec.SubnetSpec.Participants {
			subnet.ValidatorURIs = append(subnet.ValidatorURIs, nodeInfos[nodeName].Uri)
		}
		n.subnets = append(n.subnets, subnet)
	}

	return nil
}

// TeardownNetwork tears down the network constructed by the network manager and cleans up
// everything associated with it.
func (n *NetworkManager) TeardownNetwork() error {
	if err := n.initClient(); err != nil {
		return err
	}
	errs := wrappers.Errs{}
	log.Info("Shutting down cluster")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	_, err := n.anrClient.Stop(ctx)
	cancel()
	errs.Add(err)
	errs.Add(n.anrClient.Close())
	if n.serverCtxCancel != nil {
		n.serverCtxCancel()
	}
	return errs.Err
}

// CloseClient closes the connection between the ANR client and server without terminating the
// running network.
func (n *NetworkManager) CloseClient() error {
	if n.anrClient == nil {
		return nil
	}
	err := n.anrClient.Close()
	n.anrClient = nil
	return err
}

// GetSubnets returns the IDs of the currently running subnets
func (n *NetworkManager) GetSubnets() []ids.ID {
	subnetIDs := make([]ids.ID, 0, len(n.subnets))
	for _, subnet := range n.subnets {
		subnetIDs = append(subnetIDs, subnet.SubnetID)
	}
	return subnetIDs
}

// GetSubnet retrieves the subnet details for the requested subnetID
func (n *NetworkManager) GetSubnet(subnetID ids.ID) (*Subnet, bool) {
	for _, subnet := range n.subnets {
		if subnet.SubnetID == subnetID {
			return subnet, true
		}
	}
	return nil, false
}

func (n *NetworkManager) GetAllURIs(ctx context.Context) ([]string, error) {
	return n.anrClient.URIs(ctx)
}

func RegisterFiveNodeSubnetRun() func() *Subnet {
	var (
		config   = NewDefaultANRConfig()
		manager  = NewNetworkManager(config)
		numNodes = 5
	)

	_ = ginkgo.BeforeSuite(func() {
		// Name 10 new validators (which should have BLS key registered)
		subnetA := make([]string, 0)
		for i := 1; i <= numNodes; i++ {
			subnetA = append(subnetA, fmt.Sprintf("node%d-bls", i))
		}

		ctx := context.Background()
		var err error
		_, err = manager.StartDefaultNetwork(ctx)
		gomega.Expect(err).Should(gomega.BeNil())
		err = manager.SetupNetwork(
			ctx,
			config.AvalancheGoExecPath,
			[]*rpcpb.BlockchainSpec{
				{
					VmName:      evm.IDStr,
					Genesis:     "./tests/load/genesis/genesis.json",
					ChainConfig: "",
					SubnetSpec: &rpcpb.SubnetSpec{
						Participants: subnetA,
					},
				},
			},
		)
		gomega.Expect(err).Should(gomega.BeNil())
	})

	_ = ginkgo.AfterSuite(func() {
		gomega.Expect(manager).ShouldNot(gomega.BeNil())
		gomega.Expect(manager.TeardownNetwork()).Should(gomega.BeNil())
		// TODO: bootstrap an additional node to ensure that we can bootstrap the test data correctly
	})

	return func() *Subnet {
		subnetIDs := manager.GetSubnets()
		gomega.Expect(len(subnetIDs)).Should(gomega.Equal(1))
		subnetID := subnetIDs[0]
		subnetDetails, ok := manager.GetSubnet(subnetID)
		gomega.Expect(ok).Should(gomega.BeTrue())
		return subnetDetails
	}
}
