package main

import (
	"fmt"
	"os"
	"os/exec"

	appplugin "github.com/ava-labs/avalanchego/app/plugin"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/spf13/viper"
)

type nodeProcess struct {
	log       logging.Logger
	processID int
	rawClient *plugin.Client
	node      *appplugin.Client
}

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

// Stop should be called on each nodeProcess when we are done with it
func (np *nodeProcess) stop() error {
	_, err := np.node.Stop()
	np.rawClient.Kill()
	return err
}

type nodeManager struct {
	buildDirPath  string
	log           logging.Logger
	nodes         map[int]*nodeProcess
	nextProcessID int
}

func (nm *nodeManager) shutdown() {
	for _, node := range nm.nodes {
		if err := nm.stop(node.processID); err != nil {
			nm.log.Error("error stopping node: %s", err)
		}
	}
}

func (nm *nodeManager) stop(processID int) error {
	nodeProcess, exists := nm.nodes[processID]
	if !exists {
		return nil
	}
	delete(nm.nodes, processID)
	if err := nodeProcess.stop(); err != nil {
		return err
	}
	return nil
}

func newNodeManager(path string, log logging.Logger) *nodeManager {
	return &nodeManager{
		buildDirPath: path,
		log:          log,
		nodes:        map[int]*nodeProcess{},
	}
}

func (nm *nodeManager) newNode(path string, args []string, printToStdOut bool) (*nodeProcess, error) {
	config := &plugin.ClientConfig{
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
		config.SyncStdout = os.Stdout
		config.SyncStderr = os.Stderr
	}
	client := plugin.NewClient(config)
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

func (nm *nodeManager) runNormal(v *viper.Viper, nodeConfig node.Config) (int, error) {
	nm.log.Info("starting latest node version")
	node, err := nm.currentVersionNode(v, false, ids.ShortID{}, 0)
	if err != nil {
		return 1, fmt.Errorf("couldn't create current version node: %s", err)
	}
	exitCode := <-node.start()
	return exitCode, nil
}

// Start a node compatible with the previous database version
// Override the staking port, HTTP port and plugin directory of the node.
// Assumes the node binary path is [buildDir]/avalanchego-preupgrade/avalanchego-inner
// Assumes the node's plugin path is [buildDir]/avalanchego-preupgrade/plugins
// Assumes the binary can be served as a plugin
func (nm *nodeManager) preDBUpgradeNode(
	v *viper.Viper,
	stakingPort int,
	httpPort int,
) (*nodeProcess, error) {
	ignorableArgs := map[string]bool{
		config.FetchOnlyKey:   true,
		config.PluginModeKey:  true,
		config.HTTPPortKey:    true,
		config.StakingPortKey: true,
		config.PluginDirKey:   true,
	}
	args := []string{}
	for k, v := range v.AllSettings() { // Pass args to subprocess
		if ignorableArgs[k] {
			continue
		}
		args = append(args, fmt.Sprintf("--%s=%v", k, v))
	}
	args = append(args, "--plugin-mode-enabled=true") // run as a plugin
	args = append(args, fmt.Sprintf("--plugin-dir=%s/avalanchego-preupgrade/plugins", nm.buildDirPath))
	args = append(args, fmt.Sprintf("--%s=%d", config.HTTPPortKey, httpPort))
	args = append(args, fmt.Sprintf("--%s=%d", config.StakingPortKey, stakingPort))
	binaryPath := fmt.Sprintf("%s/avalanchego-preupgrade/avalanchego-inner", nm.buildDirPath)
	return nm.newNode(binaryPath, args, false)
}

func (nm *nodeManager) currentVersionNode(
	v *viper.Viper,
	fetchOnly bool,
	fetchFromNodeID ids.ShortID,
	fetchFromStakingPort int,
) (*nodeProcess, error) {
	argsMap := v.AllSettings()
	if fetchOnly {
		argsMap[config.BootstrapIPsKey] = fmt.Sprintf("127.0.0.1:%d", fetchFromStakingPort)
		argsMap[config.BootstrapIDsKey] = fmt.Sprintf("%s%s", constants.NodeIDPrefix, fetchFromNodeID)
		argsMap[config.FetchOnlyKey] = true
	}
	args := []string{}
	for k, v := range argsMap {
		args = append(args, fmt.Sprintf("--%s=%v", k, v))
	}
	binaryPath := fmt.Sprintf("%s/avalanchego-latest/avalanchego-inner", nm.buildDirPath)
	args = append(args, "--plugin-mode-enabled=true") // run as a plugin
	return nm.newNode(binaryPath, args, true)
}
