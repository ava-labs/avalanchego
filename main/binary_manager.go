package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/spf13/viper"
)

type nodeProcess struct {
	path     string
	errChan  chan error
	exitCode int
	cmd      *exec.Cmd
}

// Returns a new nodeProcess running the binary at [path].
// Returns an error if the command fails to start.
// When the nodeProcess terminates, the returned error (which may be nil)
// is sent on [n.errChan]
func startNode(path string, args []string) (*nodeProcess, error) {
	fmt.Printf("Starting binary at %s with args %s", path, args)
	n := &nodeProcess{
		path:    path,
		cmd:     exec.Command(path, args...), // #nosec G204
		errChan: make(chan error, 1),
	}
	n.cmd.Stdout = os.Stdout // TODO where to put stdout and stderr?
	n.cmd.Stderr = os.Stderr

	// Start the nodeProcess
	if err := n.cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		// Wait for the nodeProcess to stop.
		// When it does, set the exit code and send the returned error
		// (which may be nil) to [a.errChain]
		if err := n.cmd.Wait(); err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				// This code only executes if the exit code is non-zero
				n.exitCode = exitError.ExitCode()
			}
			n.errChan <- err
		}
	}()
	return n, nil
}

func (a *nodeProcess) kill() error {
	if a.cmd.Process == nil {
		return nil
	}
	//todo change this to interrupt?
	err := a.cmd.Process.Kill() // todo kill subprocesses
	if err != nil && err != os.ErrProcessDone {
		return fmt.Errorf("failed to kill process: %w", err)
	}
	return nil
}

type binaryManager struct {
	rootPath string
	log      logging.Logger
}

func newBinaryManager(path string, log logging.Logger) *binaryManager {
	return &binaryManager{
		rootPath: path,
		log:      log,
	}
}

func (b *binaryManager) runMigration(v *viper.Viper) error {
	prevVersionNode, err := b.runPreviousVersion(previousVersion, v)
	if err != nil {
		return fmt.Errorf("couldn't start old version during migration: %w", err)
	}

	currentVersionNode, err := b.runCurrentVersion(v, true)
	if err != nil {
		return fmt.Errorf("couldn't start current version during migration: %w", err)
	}

	defer func() {
		if err := prevVersionNode.kill(); err != nil {
			b.log.Error("error while killing previous version: %w", err)
		}
		if err := currentVersionNode.kill(); err != nil {
			b.log.Error("error while killing current version: %w", err)
		}
	}()

	for {
		select {
		case err := <-prevVersionNode.errChan:
			if err != nil {
				return fmt.Errorf("previous version died with exit code %d", prevVersionNode.exitCode)
			}
			if prevVersionNode.exitCode == constants.ExitCodeDoneMigrating {
				return nil
			}
			// TODO restart here
		case err := <-currentVersionNode.errChan:
			if err != nil {
				return fmt.Errorf("current version died with exit code %d", currentVersionNode.exitCode)
			}
		}
	}
}

func (b *binaryManager) runNormal(v *viper.Viper) error {
	node, err := b.runCurrentVersion(v, false)
	if err != nil {
		return fmt.Errorf("couldn't start old version during migration: %w", err)
	}
	return <-node.errChan
}

func (b *binaryManager) runPreviousVersion(prevVersion version.Version, v *viper.Viper) (*nodeProcess, error) {
	binaryPath := getBinaryPath(b.rootPath, prevVersion)
	args := []string{}
	for k, v := range v.AllSettings() {
		args = append(args, fmt.Sprintf("--%s=%v", k, v))
	}
	return startNode(binaryPath, args)
}

func (b *binaryManager) runCurrentVersion(
	v *viper.Viper,
	fetchOnly bool,
) (*nodeProcess, error) {
	binaryPath := getBinaryPath(b.rootPath, currentVersion)
	args := []string{}
	for k, v := range v.AllSettings() {
		switch {
		case fetchOnly && k == "http-port":
			args = append(args, fmt.Sprintf("--http-port=%d", v.(int)+2)) // TODO assign HTTP port better than this
		case fetchOnly && k == "staking-port":
			args = append(args, fmt.Sprintf("--staking-port=%d", v.(int)+2)) // TODO assign HTTP port better than this
		default:
			args = append(args, fmt.Sprintf("--%s=%v", k, v))
		}
		// TODO handle other command line flags
	}
	if fetchOnly {
		args = append(args, "--fetch-only=true")
	}
	return startNode(binaryPath, args)
}

func getBinaryPath(rootPath string, nodeVersion version.Version) string {
	return fmt.Sprintf(
		"%s/build/avalanchego-%s/avalanchego-inner",
		rootPath,
		nodeVersion,
	)
}
