package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	appplugin "github.com/ava-labs/avalanchego/app/plugin"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/config/versionconfig"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
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

func (nm *nodeManager) runNormal(v *viper.Viper) (int, error) {
	nm.log.Info("starting node version %s", versionconfig.NodeVersion.AsVersion())
	node, err := nm.currentVersionNode(v, false, ids.ShortID{})
	if err != nil {
		return 1, fmt.Errorf("couldn't create current version node: %s", err)
	}
	exitCode := <-node.start()
	return exitCode, nil
}

func (nm *nodeManager) preDBUpgradeNode(
	prevVersion version.Version,
	v *viper.Viper,
) (*nodeProcess, error) {
	binaryPath := getBinaryPath(nm.buildDirPath, prevVersion)
	args := []string{}
	ignorableArgs := map[string]bool{config.FetchOnlyKey: true, config.PluginModeKey: true}
	for k, v := range v.AllSettings() {
		if ignorableArgs[k] {
			continue
		}
		args = append(args, fmt.Sprintf("--%s=%v", k, v))
	}
	args = append(args, "--plugin-mode-enabled=true") // run as a plugin
	return nm.newNode(binaryPath, args, false)
}

func (nm *nodeManager) currentVersionNode(
	v *viper.Viper,
	fetchOnly bool,
	fetchFrom ids.ShortID,
) (*nodeProcess, error) {
	argsMap := v.AllSettings()
	if fetchOnly {
		stakingPortStr, ok := argsMap[config.StakingPortKey].(string)
		if !ok {
			return nil, fmt.Errorf("expected value for %s to be string but is %T", config.StakingPortKey, argsMap[config.StakingPortKey])
		}
		stakingPort, err := strconv.Atoi(stakingPortStr)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse staking port as int: %w", err)
		}
		argsMap[config.BootstrapIPsKey] = fmt.Sprintf("127.0.0.1:%d", stakingPort)
		argsMap[config.BootstrapIDsKey] = fmt.Sprintf("%s%s", constants.NodeIDPrefix, fetchFrom)
		argsMap[config.StakingPortKey] = 0
		argsMap[config.HTTPPortKey] = 0
		argsMap[config.FetchOnlyKey] = true
	}
	args := []string{}
	for k, v := range argsMap {
		args = append(args, fmt.Sprintf("--%s=%v", k, v))
	}
	binaryPath := getBinaryPath(nm.buildDirPath, versionconfig.NodeVersion.AsVersion())
	args = append(args, "--plugin-mode-enabled=true") // run as a plugin
	return nm.newNode(binaryPath, args, true)
}

func getBinaryPath(buildDirPath string, nodeVersion version.Version) string {
	return fmt.Sprintf(
		"%s/avalanchego-v%s/avalanchego-inner",
		buildDirPath,
		nodeVersion,
	)
}
