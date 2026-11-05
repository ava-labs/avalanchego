// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
)

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

// Runs the same flow as production: flags -> viper -> GetNodeConfig -> New,
// and checks that no secret value ends up in the log New writes.
func TestNewDoesNotLogSecrets(t *testing.T) {
	require := require.New(t)

	signer, err := localsigner.New()
	require.NoError(err)
	signerKey := base64.StdEncoding.EncodeToString(signer.ToBytes())

	httpsKey := base64.StdEncoding.EncodeToString([]byte("https-key-secret"))
	tlsKey := base64.StdEncoding.EncodeToString([]byte("tls-key-secret"))
	configContent := base64.StdEncoding.EncodeToString([]byte(`{"log-level":"info"}`))

	// flag -> {value passed on the command line, substring that must not be logged}
	secrets := map[string][2]string{
		config.HTTPSKeyContentKey:         {httpsKey, httpsKey},
		config.StakingTLSKeyContentKey:    {tlsKey, tlsKey},
		config.StakingSignerKeyContentKey: {signerKey, signerKey},
		config.TracingHeadersKey:          {"authorization=tracing-secret", "tracing-secret"},
		config.ConfigContentKey:           {configContent, configContent},
	}

	args := []string{
		"--" + config.NetworkNameKey + "=local",
		"--" + config.StakingEphemeralCertEnabledKey + "=true",
		"--" + config.DataDirKey + "=" + t.TempDir(),
		"--" + config.PluginDirKey + "=" + t.TempDir(),
	}
	for key, s := range secrets {
		args = append(args, "--"+key+"="+s[0])
	}

	v, err := config.BuildViper(config.BuildFlagSet(), args)
	require.NoError(err)
	nodeConfig, err := config.GetNodeConfig(v)
	require.NoError(err)

	var buf bytes.Buffer
	logger := logging.NewLogger(
		"",
		logging.NewWrappedCore(logging.Info, nopWriteCloser{&buf}, logging.JSON.ConsoleEncoder()),
	)

	_, err = New(&nodeConfig, failingLogFactory{}, logger)
	require.ErrorIs(err, errStopInitialization)

	logged := buf.String()
	require.Contains(logged, "initializing node")
	require.Contains(logged, "providedFlags")
	for key, s := range secrets {
		require.NotContains(logged, s[1], key)
	}
}
