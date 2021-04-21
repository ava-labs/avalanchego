package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	appplugin "github.com/ava-labs/avalanchego/app/plugin"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/spf13/viper"
)

// nodeProcess wraps a node client
type nodeProcess struct {
	log logging.Logger
	// Path to the binary being executed
	// must be unique among all nodeProcess
	path string
	// [rawClient].Kill() should eventually be called be called
	// on each nodeProcess
	rawClient *plugin.Client
	node      *appplugin.Client
}

// Returns a channel that the node's exit code is sent on when the node is done.
// This method does not block.
func (np *nodeProcess) start() chan int {
	np.log.Info("starting node at %s", np.path)
	exitCodeChan := make(chan int, 1)
	go func() {
		exitCode, err := np.node.Start()
		if err != nil {
			// This error could be from the subprocess shutting down
			np.log.Debug("node at path %s returned: %s", np.path, err)
		}
		exitCodeChan <- exitCode
	}()
	return exitCodeChan
}

// Stop should be called on each nodeProcess when we are done with it.
// Blocks until the node has exited.
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
	//   |_avalanchego-process (the node/binary from compiling the app directory)
	//   |_plugins
	//     |_evm
	// |_avalanchego-preupgrade
	//   |_avalanchego-process (the node/binary from compiling the app directory)
	//   |_plugins
	//     |_evm
	buildDirPath string
	log          logging.Logger
	// nodeProcess binary path --> nodeProcess
	// Must hold [lock] while touching this
	nodes map[string]*nodeProcess
	lock  sync.Mutex
}

func (nm *nodeManager) latestNodeVersionPath() string {
	return filepath.Join(nm.buildDirPath, "avalanchego-latest", "avalanchego-process")
}

func (nm *nodeManager) preupgradeNodeVersionPath() string {
	return filepath.Join(nm.buildDirPath, "avalanchego-preupgrade", "avalanchego-process")
}

// Close all running subprocesses
// Blocks until all subprocesses are ended.
func (nm *nodeManager) shutdown() {
	nm.lock.Lock()
	defer nm.lock.Unlock()

	for _, node := range nm.nodes {
		nm.log.Info("stopping node at path '%s'", node.path)
		if err := nm.stop(node.path); err != nil {
			nm.log.Error("error stopping node: %s", err)
		}
		nm.log.Info("done stopping node at path '%v'", node.path)
	}
}

// stop a node. Blocks until the node is done shutting down.
// Assumes [nm.lock] is not held
func (nm *nodeManager) Stop(path string) error {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	return nm.stop(path)
}

// stop a node. Blocks until the node is done shutting down.
// Assumes [nm.lock] is held
func (nm *nodeManager) stop(path string) error {
	nodeProcess, exists := nm.nodes[path]
	if !exists {
		return nil
	}
	delete(nm.nodes, nodeProcess.path)
	if err := nodeProcess.stop(); err != nil {
		return err
	}
	return nil
}

func newNodeManager(path string, log logging.Logger) *nodeManager {
	return &nodeManager{
		buildDirPath: path,
		log:          log,
		nodes:        map[string]*nodeProcess{},
	}
}

// Return a wrapper around a node wunning the binary at [path] with args [args].
// The returned nodeProcess must eventually have [nodeProcess.rawClient.Kill] called on it.
func (nm *nodeManager) newNode(path string, args []string, printToStdOut bool) (*nodeProcess, error) {
	nm.log.Debug("creating new node from binary at '%s'", path)
	clientConfig := &plugin.ClientConfig{
		HandshakeConfig: appplugin.Handshake,
		Plugins:         appplugin.PluginMap,
		Cmd:             exec.Command(path, args...),
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolNetRPC,
			plugin.ProtocolGRPC,
		},
		Logger: hclog.New(&hclog.LoggerOptions{Level: hclog.Error}),
	}
	if printToStdOut {
		clientConfig.SyncStdout = os.Stdout
		clientConfig.SyncStderr = os.Stderr
	}

	client := plugin.NewClient(clientConfig)
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("couldn't get client at path %s: %w", path, err)
	}

	raw, err := rpcClient.Dispense("nodeProcess")
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("couldn't dispense plugin at path %s': %w", path, err)
	}

	node, ok := raw.(*appplugin.Client)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("expected *node.NodeClient but got %T", raw)
	}

	np := &nodeProcess{
		log:       nm.log,
		node:      node,
		rawClient: client,
		path:      path,
	}
	nm.nodes[np.path] = np
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
	argsMap[config.PluginDirKey] = filepath.Join(nm.buildDirPath, "avalanchego-preupgrade", "plugins")
	args := []string{}
	for k, v := range argsMap { // Pass args to subprocess
		args = append(args, formatArgs(k, v))
	}
	binaryPath := nm.preupgradeNodeVersionPath()
	return nm.newNode(binaryPath, args, true)
}

// Run the latest node version
func (nm *nodeManager) latestVersionNodeFetchOnly(v *viper.Viper, rootConfig node.Config) (*nodeProcess, error) {
	argsMap := v.AllSettings()
	// Tell this node to run in fetch only mode and to bootstrap only from the local node
	argsMap[config.BootstrapIPsKey] = fmt.Sprintf("127.0.0.1:%d", int(rootConfig.StakingIP.Port))
	argsMap[config.BootstrapIDsKey] = fmt.Sprintf("%s%s", constants.NodeIDPrefix, rootConfig.NodeID)
	argsMap[config.FetchOnlyKey] = true
	argsMap[config.StakingPortKey] = 0   // Tell node to use any free staking port
	argsMap[config.HTTPPortKey] = 0      // Tell node to use any free HTTP port
	argsMap[config.PluginModeKey] = true // Run the node as a plugin
	argsMap[config.LogsDirKey] = filepath.Join(rootConfig.LoggingConfig.Directory, "fetch-only")
	// Make sure the node doesn't exit if the local node it's bootsrapping from doesn't respond to some messages
	argsMap[config.RetryBootstrapKey] = true
	argsMap[config.RetryBootstrapMaxAttemptsKey] = 1000
	// It's not expected that this node would bench the other node on this
	// machine, which it is bootstrapping from, but set this to be sure
	argsMap[config.BenchlistMinFailingDurationKey] = "1000h"

	var args []string
	for k, v := range argsMap {
		args = append(args, formatArgs(k, v))
	}
	binaryPath := nm.latestNodeVersionPath()
	return nm.newNode(binaryPath, args, false)
}

// Run the latest node version with the config given by [v].
// Runs until the node exits.
// Returns the node's exit code.
func (nm *nodeManager) runNormal(v *viper.Viper) (int, error) {
	nm.log.Info("starting latest node version")
	argsMap := v.AllSettings()
	argsMap[config.FetchOnlyKey] = false // don't run in fetch only mode
	argsMap[config.PluginModeKey] = true // run as plugin
	args := []string{}
	for k, v := range argsMap {
		args = append(args, formatArgs(k, v))
	}
	binaryPath := nm.latestNodeVersionPath()
	node, err := nm.newNode(binaryPath, args, true)
	if err != nil {
		return 1, fmt.Errorf("couldn't create node: %w", err)
	}
	exitCode := <-node.start()
	return exitCode, nil
}

func formatArgs(k string, v interface{}) string {
	if k == config.CorethConfigKey {
		// it either is a string, "default" or a json
		if val, ok := v.(string); ok {
			return fmt.Sprintf("--%s=%s", k, val)
		}
		// or it's a loaded config from either the defaults or a config file
		s, _ := json.Marshal(v)
		v = string(s)
	}
	return fmt.Sprintf("--%s=%v", k, v)
}
