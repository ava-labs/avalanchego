// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"bytes"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/api/admin"
	"github.com/ava-labs/avalanchego/config"
	nodeconfig "github.com/ava-labs/avalanchego/config/node"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/libevm"
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

var (
	_ logging.Factory = (*failingLogFactory)(nil)

	errStopInitialization = errors.New("stop initialization")
)

// failingLogFactory makes New return right after it logs the node
// configuration, before it starts anything.
type failingLogFactory struct {
	logging.Factory
}

func (failingLogFactory) Make(string) (logging.Logger, error) {
	return nil, errStopInitialization
}

type nopWriteCloser struct {
	*bytes.Buffer
}

func (nopWriteCloser) Close() error {
	return nil
}

// secretFlag is a flag containing a secret that must not be exposed by the
// node.
type secretFlag struct {
	flag   string // flag name
	arg    string // argument passed to [flag] on the command line
	secret string // substring of [arg] that must not appear in the output
}

func newTestNodeConfig(t *testing.T) (nodeconfig.Config, []secretFlag) {
	require := require.New(t)

	signer, err := localsigner.New()
	require.NoError(err)
	signerKey := base64.StdEncoding.EncodeToString(signer.ToBytes())

	httpsKey := base64.StdEncoding.EncodeToString([]byte("https-key-secret"))
	tlsKey := base64.StdEncoding.EncodeToString([]byte("tls-key-secret"))
	configContent := base64.StdEncoding.EncodeToString([]byte(`{"log-level":"info"}`))

	secrets := []secretFlag{
		{flag: config.HTTPSKeyContentKey, arg: httpsKey, secret: httpsKey},
		{flag: config.StakingTLSKeyContentKey, arg: tlsKey, secret: tlsKey},
		{flag: config.StakingSignerKeyContentKey, arg: signerKey, secret: signerKey},
		{flag: config.TracingHeadersKey, arg: "authorization=tracing-secret", secret: "tracing-secret"},
		{flag: config.ConfigContentKey, arg: configContent, secret: configContent},
	}

	args := []string{
		"--" + config.NetworkNameKey + "=local",
		"--" + config.StakingEphemeralCertEnabledKey + "=true",
		"--" + config.DataDirKey + "=" + t.TempDir(),
		"--" + config.PluginDirKey + "=" + t.TempDir(),
	}
	for _, s := range secrets {
		args = append(args, "--"+s.flag+"="+s.arg)
	}

	v, err := config.BuildViper(config.BuildFlagSet(), args)
	require.NoError(err)
	nodeConfig, err := config.GetNodeConfig(v)
	require.NoError(err)
	return nodeConfig, secrets
}

// Checks that no secret value ends up in the log New writes.
func TestNewDoesNotLogSecrets(t *testing.T) {
	require := require.New(t)

	nodeConfig, secrets := newTestNodeConfig(t)

	var buf bytes.Buffer
	logger := logging.NewLogger(
		"",
		logging.NewWrappedCore(logging.Info, nopWriteCloser{&buf}, logging.JSON.ConsoleEncoder()),
	)

	_, err := New(&nodeConfig, failingLogFactory{}, logger)
	require.ErrorIs(err, errStopInitialization)

	logged := buf.String()
	require.Contains(logged, "initializing node")
	require.Contains(logged, "providedFlags")
	for _, s := range secrets {
		require.NotContains(logged, s.secret, s.flag)
	}
}

// Checks that admin.getConfig does not leak secrets through the HTTP service.
func TestAdminGetConfigDoesNotLeakSecrets(t *testing.T) {
	require := require.New(t)

	nodeConfig, secrets := newTestNodeConfig(t)

	service, err := admin.NewService(admin.Config{
		Log:        logging.NoLog{},
		NodeConfig: &nodeConfig,
	})
	require.NoError(err)

	body := `{"jsonrpc":"2.0","id":1,"method":"admin.getConfig","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	service.ServeHTTP(w, req)
	require.Equal(http.StatusOK, w.Code)

	// Make sure we got the config and not an error response.
	resp := w.Body.String()
	require.Contains(resp, "traceConfig")
	for _, s := range secrets {
		require.NotContains(resp, s.secret, s.flag)
	}
}
