package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/spf13/viper"
)

type nodeProcess struct {
	path       string
	errChan    chan error
	done       *utils.AtomicBool
	exitCode   int
	cmd        *exec.Cmd
	nodeNumber int
}

type binaryManager struct {
	buildDirPath   string
	log            logging.Logger
	nextNodeNumber int
	runningNodes   map[int]*nodeProcess
}

func newBinaryManager(path string, log logging.Logger) *binaryManager {
	return &binaryManager{
		buildDirPath: path,
		log:          log,
		runningNodes: map[int]*nodeProcess{},
	}
}

func (b *binaryManager) killAll() {
	for _, nodeProcess := range b.runningNodes {
		if err := b.kill(nodeProcess.nodeNumber); err != nil {
			b.log.Error("error killing node running binary at %s: %s", nodeProcess.path, err)
		}
	}
}

func (b *binaryManager) kill(nodeNumber int) error {
	nodeProcess, ok := b.runningNodes[nodeNumber]
	if !ok {
		return nil
	}
	delete(b.runningNodes, nodeNumber)
	if nodeProcess.cmd.Process == nil || nodeProcess.done.GetValue() {
		return nil
	}
	// Stop printing output from node
	nodeProcess.cmd.Stdout = ioutil.Discard
	nodeProcess.cmd.Stderr = ioutil.Discard
	err := syscall.Kill(-nodeProcess.cmd.Process.Pid, syscall.SIGKILL)
	if err != nil && err != os.ErrProcessDone {
		return fmt.Errorf("failed to kill process: %w", err)
	}
	return nil
}

// Returns a new nodeProcess running the binary at [path].
// Returns an error if the command fails to start.
// When the nodeProcess terminates, the returned error (which may be nil)
// is sent on [n.errChan]
func (b *binaryManager) startNode(path string, args []string, printToStdOut bool) (*nodeProcess, error) {
	b.log.Info("Starting binary at %s with args %s\n", path, args) // TODO remove
	n := &nodeProcess{
		path:       path,
		cmd:        exec.Command(path, args...), // #nosec G204
		errChan:    make(chan error, 1),
		done:       &utils.AtomicBool{},
		nodeNumber: b.nextNodeNumber,
	}
	b.nextNodeNumber++
	if printToStdOut {
		n.cmd.Stdout = os.Stdout
		n.cmd.Stderr = os.Stderr
	}
	n.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Start the nodeProcess
	if err := n.cmd.Start(); err != nil {
		return nil, err
	}
	b.runningNodes[n.nodeNumber] = n
	go func() {
		// Wait for the nodeProcess to stop.
		// When it does, set the exit code and send the returned error
		// (which may be nil) to [a.errChan]
		if err := n.cmd.Wait(); err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				// This code only executes if the exit code is non-zero
				n.exitCode = exitError.ExitCode()
			}
			n.done.SetValue(true)
			n.errChan <- err
		}
	}()
	return n, nil
}

// Run two nodes at once: one is a version before the database upgrade and the other after.
// The latter will bootstrap from the former. Its staking port and HTTP port are 2
// greater than the staking/HTTP ports in [v].
// When the new node version is done bootstrapping, both nodes are stopped.
// Returns nil if the new node version successfully bootstrapped.
func (b *binaryManager) runMigration(v *viper.Viper, nodeConfig node.Config) error {
	prevVersionNode, err := b.runPreviousVersion(node.PreviousVersion.AsVersion(), v)
	if err != nil {
		return fmt.Errorf("couldn't start old version during migration: %w", err)
	}
	defer func() {
		if err := b.kill(prevVersionNode.nodeNumber); err != nil {
			b.log.Error("error while killing previous version: %s", err)
		}
	}()

	currentVersionNode, err := b.runCurrentVersion(v, true, nodeConfig.NodeID)
	if err != nil {
		return fmt.Errorf("couldn't start current version during migration: %w", err)
	}
	defer func() {
		if err := b.kill(currentVersionNode.nodeNumber); err != nil {
			b.log.Error("error while killing current version: %s", err)
		}
	}()

	for {
		select {
		case err := <-prevVersionNode.errChan:
			return fmt.Errorf("previous version stopped with: %s", err)
		case <-currentVersionNode.errChan:
			if currentVersionNode.exitCode != constants.ExitCodeDoneMigrating {
				return fmt.Errorf("current version died with exit code %d", currentVersionNode.exitCode)
			}
			return nil
		}
	}
}

func (b *binaryManager) runNormal(v *viper.Viper) error {
	node, err := b.runCurrentVersion(v, false, ids.ShortID{})
	if err != nil {
		return fmt.Errorf("couldn't start old version during migration: %w", err)
	}
	defer func() {
		if err := b.kill(node.nodeNumber); err != nil {
			b.log.Error("error stopping node: %s", err)
		}
	}()
	return <-node.errChan
}

func (b *binaryManager) runPreviousVersion(prevVersion version.Version, v *viper.Viper) (*nodeProcess, error) {
	binaryPath := getBinaryPath(b.buildDirPath, prevVersion)
	args := []string{}
	for k, v := range v.AllSettings() {
		if k == "fetch-only" { // TODO replace with const
			continue
		}
		args = append(args, fmt.Sprintf("--%s=%v", k, v))
	}
	return b.startNode(binaryPath, args, false)
}

func (b *binaryManager) runCurrentVersion(
	v *viper.Viper,
	fetchOnly bool,
	fetchFrom ids.ShortID,
) (*nodeProcess, error) {
	argsMap := v.AllSettings()
	if fetchOnly {
		// TODO use constants for arg names here
		stakingPort, err := strconv.Atoi(argsMap["staking-port"].(string))
		if err != nil {
			return nil, fmt.Errorf("couldn't parse staking port as int: %w", err)
		}
		argsMap["bootstrap-ips"] = fmt.Sprintf("127.0.0.1:%d", stakingPort)
		argsMap["bootstrap-ids"] = fmt.Sprintf("%s%s", constants.NodeIDPrefix, fetchFrom)
		argsMap["staking-port"] = stakingPort + 2

		httpPort, err := strconv.Atoi(argsMap["http-port"].(string))
		if err != nil {
			return nil, fmt.Errorf("couldn't parse staking port as int: %w", err)
		}
		argsMap["http-port"] = httpPort + 2
		argsMap["fetch-only"] = true
	}
	args := []string{}
	for k, v := range argsMap {
		args = append(args, fmt.Sprintf("--%s=%v", k, v))
	}
	binaryPath := getBinaryPath(b.buildDirPath, node.Version.AsVersion())
	return b.startNode(binaryPath, args, true)
}

func getBinaryPath(buildDirPath string, nodeVersion version.Version) string {
	return fmt.Sprintf(
		"%s/avalanchego-%s/avalanchego-inner",
		buildDirPath,
		nodeVersion,
	)
}
