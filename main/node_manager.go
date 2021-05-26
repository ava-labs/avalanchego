package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	appplugin "github.com/ava-labs/avalanchego/app/plugin"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
)

const (
	latestVersionDir     = "avalanchego-latest"
	preupgradeVersionDir = "avalanchego-preupgrade"
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
// This method blocks until the node starts.
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
	nodes       map[string]*nodeProcess
	lock        sync.Mutex
	hasShutdown bool
}

// Close all running subprocesses
// Blocks until all subprocesses are ended.
// Invocations of this method after the first invocation do nothing.
func (nm *nodeManager) shutdown() {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	if nm.hasShutdown {
		return
	}
	nm.hasShutdown = true

	for _, node := range nm.nodes {
		nm.log.Info("stopping node at path '%s'", node.path)
		if err := nm.stop(node.path); err != nil {
			nm.log.Error("error stopping node: %s", err)
		}
		nm.log.Info("done stopping node at path '%v'", node.path)
	}
}

// Stop a node. Blocks until the node is done shutting down.
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

// Return a wrapper around a node that will run the binary at [path] with args [args].
// The returned nodeProcess must eventually have [nodeProcess.rawClient.Kill] called on it.
// Assumes [nm.lock] is held
func (nm *nodeManager) newNode(path string, args []string, printToStdOut bool) (*nodeProcess, error) {
	nm.log.Debug("creating new node from binary at '%s'", path)
	clientConfig := &plugin.ClientConfig{
		HandshakeConfig:  appplugin.Handshake,
		Plugins:          appplugin.PluginMap,
		Cmd:              exec.Command(path, args...),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		Logger:           hclog.New(&hclog.LoggerOptions{Level: hclog.Error}),
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

// Return a node compatible with the previous database version.
// Assumes the node binary path is [buildDir]/avalanchego-preupgrade/avalanchego-process
// Assumes the node's plugin path is [buildDir]/avalanchego-preupgrade/plugins
// Assumes the binary can be served as a plugin.
func (nm *nodeManager) preDBUpgradeNode() (*nodeProcess, error) {
	args := make([]string, len(os.Args)+1)
	copy(args, os.Args[1:])
	args = append(
		args,
		fmt.Sprintf("--%s=%s", config.PluginModeKey, "true"),
		fmt.Sprintf("--%s=%s", config.BuildDirKey, nm.buildDirPath),
	)

	binaryPath := filepath.Join(nm.buildDirPath, preupgradeVersionDir, "avalanchego-process")
	return nm.newNode(binaryPath, args, true)
}

// Return the latest version node, override configs to run in fetch-only mode.
func (nm *nodeManager) latestVersionNodeFetchOnly(rootConfig node.Config) (*nodeProcess, error) {
	nodeID, err := ids.ToShortID(hashing.PubkeyBytesToAddress(rootConfig.StakingTLSCert.Leaf.Raw))
	if err != nil {
		return nil, fmt.Errorf("couldn't parse node ID: %w", err)
	}
	args := make([]string, len(os.Args)+9)
	copy(args, os.Args[1:])
	args = append(
		args,
		// Tell this node to run in fetch only mode and to bootstrap only from the local node
		fmt.Sprintf("--%s=127.0.0.1:%d", config.BootstrapIPsKey, int(rootConfig.StakingIP.Port)),
		fmt.Sprintf("--%s=%s%s", config.BootstrapIDsKey, constants.NodeIDPrefix, nodeID),
		fmt.Sprintf("--%s=%s", config.FetchOnlyKey, "true"),
		fmt.Sprintf("--%s=%d", config.StakingPortKey, 0), // use any available port for staking port
		fmt.Sprintf("--%s=%d", config.HTTPPortKey, 0),    // use any available port for HTTP port
		fmt.Sprintf("--%s=%s", config.PluginModeKey, "true"),
		fmt.Sprintf("--%s=%s", config.LogsDirKey, filepath.Join(rootConfig.LoggingConfig.Directory, "fetch-only")),
		fmt.Sprintf("--%s=%s", config.RetryBootstrapKey, "true"),
		fmt.Sprintf("--%s=%d", config.RetryBootstrapMaxAttemptsKey, 1000),
		fmt.Sprintf("--%s=%s", config.BenchlistMinFailingDurationKey, "1000h"),
	)

	binaryPath := filepath.Join(nm.buildDirPath, latestVersionDir, "avalanchego-process")
	return nm.newNode(binaryPath, args, false)
}

// Run the latest node version in plugin-mode and with the supplied configurations.
// Runs until the node exits.
// Returns the node's exit code.
func (nm *nodeManager) runNormal() (int, error) {
	nm.lock.Lock()
	if nm.hasShutdown {
		nm.lock.Unlock()
		return 0, nil
	}
	nm.log.Info("starting latest node version in normal execution mode")

	args := make([]string, len(os.Args)+1)
	copy(args, os.Args[1:])
	args = append(
		args,
		fmt.Sprintf("--%s=false", config.FetchOnlyKey), // don't run in fetch only mode
		fmt.Sprintf("--%s=true", config.PluginModeKey), // run as plugin
	)

	binaryPath := filepath.Join(nm.buildDirPath, latestVersionDir, "avalanchego-process")
	node, err := nm.newNode(binaryPath, args, true)
	if err != nil {
		nm.lock.Unlock()
		return 1, fmt.Errorf("couldn't create node: %w", err)
	}
	exitCodeChan := node.start()
	nm.lock.Unlock()

	return <-exitCodeChan, nil
}
