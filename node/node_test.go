// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm"
)

// TestShutdownLeaksNoGoroutines tests that Node does not start any goroutines
// that outlive it.
func TestShutdownLeaksNoGoroutines(t *testing.T) {
	v, err := config.BuildViper(config.BuildFlagSet(), []string{
		"--network-id=local",
		"--data-dir=" + t.TempDir(),
		"--db-type=memdb",
		"--public-ip=127.0.0.1",
		"--staking-port=0",
		"--http-port=0",
	})
	require.NoError(t, err)

	cfg, err := config.GetNodeConfig(v)
	require.NoError(t, err)

	logs := logging.NewFactory(cfg.LoggingConfig)
	log, err := logs.Make("main")
	require.NoError(t, err)

	// New builds the C-Chain, which needs libevm's extras. libevm takes them
	// once per process, so the test holds them only while the node runs.
	err = libevm.WithTempRegisteredExtras(func() error {
		n, err := New(&cfg, logs, log)
		if err != nil {
			return err
		}

		n.Shutdown(0)
		return nil
	})
	require.NoError(t, err)

	logs.Close()

	opts := []goleak.Option{
		// libevm starts these at import and never stops them.
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/core.(*txSenderCacher).cache"),
		// libevm starts one meter ticker per process and never stops it.
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/metrics.(*meterArbiter).tick"),
		// lumberjack's Close does not stop the goroutine that rotates files.
		goleak.IgnoreTopFunction("gopkg.in/natefinch/lumberjack%2ev2.(*Logger).millRun"),
	}
	goleak.VerifyNone(t, opts...)
}
