package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	appplugin "github.com/ava-labs/avalanchego/app/plugin"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/node"
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
			// Couldn't start the node
			np.log.Error("couldn't start node: %w", err)
			exitCodeChan <- 1
			return
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

type nodeProcessManager struct {
	buildDirPath  string
	log           logging.Logger
	nodes         map[int]*nodeProcess
	nextProcessID int
}

func (b *nodeProcessManager) shutdown() {
	for _, node := range b.nodes {
		if err := b.stop(node.processID); err != nil {
			b.log.Error("error stopping node: %s", err)
		}
	}
}

func (b *nodeProcessManager) stop(processID int) error {
	nodeProcess, exists := b.nodes[processID]
	if !exists {
		return nil
	}
	delete(b.nodes, processID)
	if err := nodeProcess.stop(); err != nil {
		return err
	}
	return nil
}

func newNodeProcessManager(path string, log logging.Logger) *nodeProcessManager {
	return &nodeProcessManager{
		buildDirPath: path,
		log:          log,
		nodes:        map[int]*nodeProcess{},
	}
}

func (b *nodeProcessManager) newNode(path string, args []string, printToStdOut bool) (*nodeProcess, error) {
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
	b.nextProcessID++
	np := &nodeProcess{
		log:       b.log,
		node:      node,
		rawClient: client,
		processID: b.nextProcessID,
	}
	b.nodes[np.processID] = np
	return np, nil
}

func (b *nodeProcessManager) runNormal(v *viper.Viper) (int, error) {
	b.log.Info("starting node version %s", node.Version.AsVersion())
	node, err := b.currentVersionNode(v, false, ids.ShortID{})
	if err != nil {
		return 1, fmt.Errorf("couldn't create current version node: %s", err)
	}
	exitCode := <-node.start()
	return exitCode, nil
}

func (b *nodeProcessManager) previousVersionNode(
	prevVersion version.Version,
	v *viper.Viper,
) (*nodeProcess, error) {
	binaryPath := getBinaryPath(b.buildDirPath, prevVersion)
	args := []string{}
	ignorableArgs := map[string]bool{config.FetchOnlyKey: true, config.PluginModeKey: true}
	for k, v := range v.AllSettings() {
		if ignorableArgs[k] {
			continue
		}
		args = append(args, fmt.Sprintf("--%s=%v", k, v))
	}
	args = append(args, "--plugin-mode-enabled=true") //ensure it's running as a plugin
	return b.newNode(binaryPath, args, false)
}

func (b *nodeProcessManager) currentVersionNode(
	v *viper.Viper,
	fetchOnly bool,
	fetchFrom ids.ShortID,
) (*nodeProcess, error) {
	argsMap := v.AllSettings()
	if fetchOnly {
		stakingPort, err := strconv.Atoi(argsMap["staking-port"].(string))
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
	binaryPath := getBinaryPath(b.buildDirPath, node.Version.AsVersion())
	return b.newNode(binaryPath, args, true)
}

func getBinaryPath(buildDirPath string, nodeVersion version.Version) string {
	return fmt.Sprintf(
		"%s/avalanchego-v%s/avalanchego-inner",
		buildDirPath,
		nodeVersion,
	)
}
