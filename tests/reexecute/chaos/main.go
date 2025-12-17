// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/reexecute"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	blockDirArg        string
	currentStateDirArg string
	startBlockArg      uint64
	endBlockArg        uint64
	minWaitTimeArg     time.Duration
	maxWaitTimeArg     time.Duration

	firewoodConfig = `{
		"state-scheme": "firewood",
		"snapshot-cache": 0,
		"pruning-enabled": true,
		"state-sync-enabled": false
	}`
)

func init() {
	evm.RegisterAllLibEVMExtras()

	flag.StringVar(&blockDirArg, "block-dir", blockDirArg, "Block DB directory to read from during re-execution.")
	flag.StringVar(&currentStateDirArg, "current-state-dir", currentStateDirArg, "Current state directory including VM DB and Chain Data Directory for re-execution.")
	flag.Uint64Var(&startBlockArg, "start-block", 101, "Start block to begin execution (exclusive).")
	flag.Uint64Var(&endBlockArg, "end-block", 200, "End block to end execution (inclusive).")
	flag.DurationVar(&minWaitTimeArg, "min-wait-time", 20*time.Second, "Minimum amount of time to wait before crashing.")
	flag.DurationVar(&maxWaitTimeArg, "max-wait-time", 30*time.Second, "Maximum amount of time to wait before crashing.")

	flag.Parse()
}

func main() {
	tc := tests.NewTestContext(tests.NewDefaultLogger("chaos-test"))
	tc.SetDefaultContextParent(context.Background())
	tc.RecoverAndExit()

	run(
		tc,
		minWaitTimeArg,
		maxWaitTimeArg,
		blockDirArg,
		currentStateDirArg,
		startBlockArg,
		endBlockArg,
	)
}

// run executes a chaos test that simulates an application crash during C-Chain
// block reexecution that uses Firewood. It verifies that the VM can recover from
// an unexpected termination and resume processing from the correct block height
// using persisted state.
//
// Running the chaos test involves a few steps:
//  1. Start a reexecution test process using the Firewood state scheme
//  2. Allow the reexecution test to run for the specified wait duration
//  3. Forcefully terminate the process with SIGKILL to simulate a crash
//  4. Open the VM database to read the last accepted block height from persisted state
//  5. Restart the reexecution test from the recovered height to verify state consistency
func run(
	tc tests.TestContext,
	minWaitTime time.Duration,
	maxWaitTime time.Duration,
	blockDir string,
	currentStateDir string,
	startBlock uint64,
	endBlock uint64,
) {
	r := require.New(tc)
	log := tc.Log()

	cmd := createReexecutionCmd(blockDir, currentStateDir, startBlock, endBlock)
	// Set process group ID so we can kill all child processes
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// 1. Start a reexecution test process using the Firewood state scheme
	r.NoError(cmd.Start())

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// 2. Allow the reexecution test to run for the specified wait duration
	waitTime := time.Duration(rand.Int63n(int64(maxWaitTime-minWaitTime)+1)) + minWaitTime
	log.Debug("started reexecution test", zap.Duration("wait time", waitTime))

	time.Sleep(waitTime)

	// 3. Forcefully terminate the process with SIGKILL to simulate a crash
	select {
	case waitErr := <-done:
		r.FailNow("reexecution test terminated prior to crash test", zap.Error(waitErr))
	default:
		pgid, err := syscall.Getpgid(cmd.Process.Pid)
		r.NoError(err)

		log.Debug("killing reexecution test")

		r.NoError(syscall.Kill(-pgid, syscall.SIGKILL))

		waitCtx := tc.DefaultContext()

		var waitErr error
		select {
		case err := <-done:
			waitErr = err
		case <-waitCtx.Done():
			r.FailNow("timed out waiting for killed process to terminate")
		}

		exitErr, ok := waitErr.(*exec.ExitError)
		r.True(ok)

		// ExitCode() returns -1 when killed by signal
		r.Equal(-1, exitErr.ProcessState.ExitCode(), "unexpected exit code after kill")
	}

	var (
		vmDBDir      = filepath.Join(currentStateDir, "db")
		chainDataDir = filepath.Join(currentStateDir, "chain-data-dir")
	)

	// 4. Open the VM database to read the last accepted block height from persisted state
	db, err := openDB(vmDBDir, 10)
	r.NoError(err)

	ctx := tc.GetDefaultContextParent()
	vm, err := reexecute.NewMainnetCChainVM(
		ctx,
		db,
		chainDataDir,
		[]byte(firewoodConfig),
		metrics.NewPrefixGatherer(),
		prometheus.NewRegistry(),
	)
	r.NoError(err)

	lastAcceptedID, err := vm.LastAccepted(ctx)
	r.NoError(err)

	lastAcceptedBlock, err := vm.GetBlock(ctx, lastAcceptedID)
	r.NoError(err)

	r.NoError(vm.Shutdown(ctx))
	r.NoError(db.Close())

	log.Debug("read VM", zap.Uint64("latest height", lastAcceptedBlock.Height()))

	cmd = createReexecutionCmd(blockDir, currentStateDir, lastAcceptedBlock.Height()+1, endBlock)

	// 5. Restart the reexecution test from the recovered height to verify state consistency
	r.NoError(cmd.Run())
}

// openDB attempts to open a LevelDB database with retry logic and linear backoff.
// This is necessary after killing a process that held the database open, as the OS may
// need time to release file locks even after the process terminates.
//
// The backoff strategy increases by 500ms per attempt (500ms, 1s, 1.5s, 2s, ...).
func openDB(dbDir string, maxAttempts int) (database.Database, error) {
	attempt := 0
	for {
		db, err := leveldb.New(dbDir, nil, logging.NoLog{}, prometheus.NewRegistry())
		if err == nil {
			return db, nil
		}

		attempt += 1
		if attempt == maxAttempts {
			return nil, fmt.Errorf("failed to reopen db after %d attempts: %w", maxAttempts, err)
		}

		backoff := time.Duration(attempt) * 500 * time.Millisecond
		time.Sleep(backoff)
	}
}

// createReexecutionCmd constructs a command to run the C-Chain reexecution test.
func createReexecutionCmd(
	blockDir string,
	currentStateDir string,
	startBlock uint64,
	endBlock uint64,
) *exec.Cmd {
	cmd := exec.Command("go",
		"run",
		"github.com/ava-labs/avalanchego/tests/reexecute/c",
		"--config=firewood",
		"--block-dir="+blockDir,
		"--current-state-dir="+currentStateDir,
		"--start-block="+strconv.Itoa(int(startBlock)),
		"--end-block="+strconv.Itoa(int(endBlock)),
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd
}
