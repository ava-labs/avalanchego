package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	appplugin "github.com/ava-labs/avalanchego/app/plugin"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/spf13/viper"
)

// nodeProcess wraps a node client
type nodeProcess struct {
	log logging.Logger
	// Every nodeProcess has a unique ID
	processID int
	// [rawClient].Kill() should eventually be called be called
	// on each nodeProcess
	rawClient *plugin.Client
	node      *appplugin.Client
}

// Returns a channel that the node's exit code is sent on when the node is done
// This method does not block.
func (np *nodeProcess) start() chan int {
	exitCodeChan := make(chan int, 1)
	go func() {
		exitCode, err := np.node.Start()
		if err != nil {
			// This error could be from the subprocess shutting down
			np.log.Debug("node returned: %s", err)
		}
		exitCodeChan <- exitCode
	}()
	return exitCodeChan
}

// Stop should be called on each nodeProcess when we are done with it.
// Calls [Kill()] on the underlying client.
func (np *nodeProcess) stop() error {
	err := np.node.Stop()
	np.rawClient.Kill()
	return err
}

type nodeManager struct {
	// Path to the build directory, which should have this structure:
	// build
	// |_avalanchego-latest
	//   |_avalanchego-process (the binary from compiling the app directory)
	//   |_plugins
	//     |_evm
	// |_avalanchego-preupgrade
	//   |_avalanchego-process (the binary from compiling the app directory)
	//   |_plugins
	//     |_evm
	buildDirPath string
	log          logging.Logger
	// nodeProcess ID --> nodeProcess
	nodes         map[int]*nodeProcess
	nextProcessID int
	processLock   sync.Mutex
}

func (nm *nodeManager) latestNodeVersionPath() string {
	return fmt.Sprintf("%s/avalanchego-latest/avalanchego-process", nm.buildDirPath)
}

func (nm *nodeManager) preupgradeNodeVersionPath() string {
	return fmt.Sprintf("%s/avalanchego-preupgrade/avalanchego-process", nm.buildDirPath)
}

func (nm *nodeManager) shutdown() {
	nm.processLock.Lock()
	defer nm.processLock.Unlock()

	for _, node := range nm.nodes {
		nm.log.Info("stopping process %v", node.processID)
		if err := nm.stop(node.processID); err != nil {
			nm.log.Error("error stopping node: %s", err)
		}
		nm.log.Info("done stopping process %v", node.processID)
	}
}

// stop a node. Blocks until the node is done shutting down.
// Assumes [nm.processLock] is not held
func (nm *nodeManager) Stop(processID int) error {
	nm.processLock.Lock()
	defer nm.processLock.Unlock()
	return nm.stop(processID)
}

// stop a node. Blocks until the node is done shutting down.
// Assumes [nm.processLock] is held
func (nm *nodeManager) stop(processID int) error {
	nodeProcess, exists := nm.nodes[processID]
	if !exists {
		return nil
	}
	if err := nodeProcess.stop(); err != nil {
		return err
	}
	delete(nm.nodes, processID)
	return nil
}

func newNodeManager(path string, log logging.Logger) *nodeManager {
	return &nodeManager{
		buildDirPath: path,
		log:          log,
		nodes:        map[int]*nodeProcess{},
		processLock:  sync.Mutex{},
	}
}

// Return a wrapper around a node wunning the binary at [path] with args [args].
// The returned nodeProcess must eventually have [nodeProcess.rawClient.Kill] called on it.
func (nm *nodeManager) newNode(path string, args []string, printToStdOut bool) (*nodeProcess, error) {
	clientConfig := &plugin.ClientConfig{
		HandshakeConfig: appplugin.Handshake,
		Plugins:         appplugin.PluginMap,
		Cmd:             exec.Command(path, args...),
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolNetRPC,
			plugin.ProtocolGRPC,
		},
		Logger: hclog.New(&hclog.LoggerOptions{Level: hclog.Warn}),
	}
	if printToStdOut {
		clientConfig.SyncStdout = os.Stdout
		clientConfig.SyncStderr = os.Stderr
	}
	client := plugin.NewClient(clientConfig)
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("couldn't get Client: %w", err)
	}
	raw, err := rpcClient.Dispense("nodeProcess")
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("couldn't dispense plugin '%s': %w", "nodeProcess", err)
	}
	node, ok := raw.(*appplugin.Client)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("expected *node.NodeClient but got %T", raw)
	}
	nm.nextProcessID++
	np := &nodeProcess{
		log:       nm.log,
		node:      node,
		rawClient: client,
		processID: nm.nextProcessID,
	}
	nm.nodes[np.processID] = np
	return np, nil
}

// Start a node compatible with the previous database version
// Override the staking port, HTTP port and plugin directory of the node.
// Assumes the node binary path is [buildDir]/avalanchego-preupgrade/avalanchego-process
// Assumes the node's plugin path is [buildDir]/avalanchego-preupgrade/plugins
// Assumes the binary can be served as a plugin
func (nm *nodeManager) preDBUpgradeNode(v *viper.Viper) (*nodeProcess, error) {
	argsMap := v.AllSettings()
	delete(argsMap, config.FetchOnlyKey)
	argsMap[config.PluginModeKey] = true
	argsMap[config.PluginDirKey] = fmt.Sprintf("%s/avalanchego-preupgrade/plugins", nm.buildDirPath)
	args := []string{}
	for k, v := range argsMap { // Pass args to subprocess
		args = append(args, fmt.Sprintf("--%s=%v", k, v))
	}
	binaryPath := nm.preupgradeNodeVersionPath()
	return nm.newNode(binaryPath, args, true)
}

// Run the latest node version
func (nm *nodeManager) latestVersionNodeFetchOnly(
	v *viper.Viper,
	fetchFromNodeID ids.ShortID,
	fetchFromStakingPort int,
) (*nodeProcess, error) {
	argsMap := v.AllSettings()
	argsMap[config.BootstrapIPsKey] = fmt.Sprintf("127.0.0.1:%d", fetchFromStakingPort)
	argsMap[config.BootstrapIDsKey] = fmt.Sprintf("%s%s", constants.NodeIDPrefix, fetchFromNodeID)
	argsMap[config.FetchOnlyKey] = true
	argsMap[config.StakingPortKey] = 0
	argsMap[config.HTTPPortKey] = 0
	argsMap[config.PluginModeKey] = true
	args := []string{}
	for k, v := range argsMap {
		args = append(args, fmt.Sprintf("--%s=%v", k, v))
	}
	binaryPath := nm.latestNodeVersionPath()
	return nm.newNode(binaryPath, args, false)
}

// Run the latest node version with the config given by [v].
// Returns the node's exit code.
func (nm *nodeManager) runNormal(v *viper.Viper) (int, error) {
	nm.log.Info("starting latest node version")
	argsMap := v.AllSettings()
	argsMap[config.FetchOnlyKey] = false // don't run in fetch only mode
	argsMap[config.PluginModeKey] = true // run as plugin
	args := []string{}
	for k, v := range argsMap {
		args = append(args, fmt.Sprintf("--%s=%v", k, v))
	}
	binaryPath := nm.latestNodeVersionPath()
	node, err := nm.newNode(binaryPath, args, true)
	if err != nil {
		return 1, fmt.Errorf("couldn't create node: %w", err)
	}
	exitCode := <-node.start()
	return exitCode, nil
}
