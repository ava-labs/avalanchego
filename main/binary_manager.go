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

type process struct {
	path      string
	errChan   chan error
	done      *utils.AtomicBool
	exitCode  int
	cmd       *exec.Cmd
	processID int
}

type binaryManager struct {
	buildDirPath  string
	log           logging.Logger
	nextProcessID int
	runningNodes  map[int]*process
}

func newBinaryManager(path string, log logging.Logger) *binaryManager {
	return &binaryManager{
		buildDirPath: path,
		log:          log,
		runningNodes: map[int]*process{},
	}
}

func (b *binaryManager) killAll() {
	for _, nodeProcess := range b.runningNodes {
		if err := b.kill(nodeProcess.processID); err != nil {
			b.log.Error("error killing node running binary at %s: %s", nodeProcess.path, err)
		}
	}
}

func (b *binaryManager) kill(nodeNumber int) error {
	nodeProcess, ok := b.runningNodes[nodeNumber]
	if !ok {
		return nil
	}
	// Stop printing output from node
	nodeProcess.cmd.Stdout = ioutil.Discard
	nodeProcess.cmd.Stderr = ioutil.Discard
	delete(b.runningNodes, nodeNumber)
	if nodeProcess.cmd.Process == nil || nodeProcess.done.GetValue() {
		return nil
	}

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
func (b *binaryManager) startNode(path string, args []string, printToStdOut bool) (*process, error) {
	b.log.Info("Starting binary at %s with args %s\n", path, args) // TODO remove
	n := &process{
		path:      path,
		cmd:       exec.Command(path, args...), // #nosec G204
		errChan:   make(chan error, 1),
		done:      &utils.AtomicBool{},
		processID: b.nextProcessID,
	}
	b.nextProcessID++
	if printToStdOut {
		n.cmd.Stdout = os.Stdout
		n.cmd.Stderr = os.Stderr
	}
	n.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Start the nodeProcess
	if err := n.cmd.Start(); err != nil {
		return nil, err
	}
	b.runningNodes[n.processID] = n
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

func (b *binaryManager) runNormal(v *viper.Viper) error {
	node, err := b.runCurrentVersion(v, false, ids.ShortID{})
	if err != nil {
		return fmt.Errorf("couldn't start old version during migration: %w", err)
	}
	defer func() {
		if err := b.kill(node.processID); err != nil {
			b.log.Error("error stopping node: %s", err)
		}
	}()
	return <-node.errChan
}

func (b *binaryManager) runPreviousVersion(prevVersion version.Version, v *viper.Viper) (*process, error) {
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
) (*process, error) {
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
		"%s/avalanchego-v%s/avalanchego-inner",
		buildDirPath,
		nodeVersion,
	)
}
