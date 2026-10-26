// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

func TestAlloyConfig(t *testing.T) {
	got := alloyConfig(collectorConfig{url: "https://tempo-x.grafana.net/tempo", username: "123", password: "secret"})
	for _, want := range []string{
		`otelcol.receiver.otlp "nodes"`,
		`endpoint = "127.0.0.1:4317"`,
		`otelcol.processor.batch "default"`,
		`otelcol.auth.basic "tempo"`,
		`username = "123"`,
		`password = "secret"`,
		`otelcol.exporter.otlphttp "tempo"`,
		`endpoint = "https://tempo-x.grafana.net/tempo"`,
		`auth     = otelcol.auth.basic.tempo.handler`,
	} {
		require.Contains(t, got, want, "alloyConfig() fragment")
	}
	if _, err := exec.LookPath("alloy"); err == nil {
		// alloy fmt reads the file and exits non-zero on syntax errors
		path := filepath.Join(t.TempDir(), "config.alloy")
		require.NoError(t, os.WriteFile(path, []byte(got), perms.ReadWrite), "WriteFile()")
		require.NoError(t, exec.CommandContext(t.Context(), "alloy", "fmt", path).Run(), "alloy fmt")
	}
}

func TestValidateTempoPushURL(t *testing.T) {
	require.NoError(t, validateTempoPushURL("https://tempo-x.grafana.net/tempo"), "valid base URL")
	require.ErrorIs(t, validateTempoPushURL("https://tempo-x.grafana.net/tempo/v1/traces"), errTempoPushURLHasPath, "URL with /v1/traces")
	require.ErrorIs(t, validateTempoPushURL("ftp://x"), errTempoPushURLScheme, "non-http scheme")
}

func TestAlloyRunningWithoutPIDFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // getTmpnetPath uses os.UserHomeDir, which reads HOME on unix
	running, err := alloyRunning()
	require.NoError(t, err, "alloyRunning()")
	require.False(t, running, "no PID file => not running")
}

func TestTracingFlagsForLabels(t *testing.T) {
	flags, err := tracingFlagsForLabels(map[string]string{
		"instance": "drop-me", "network_uuid": "abc", "node_id": "NodeID-1", "gh_run_id": "7",
	})
	require.NoError(t, err, "tracingFlagsForLabels()")
	require.Equal(t, FlagsMap{
		config.TracingExporterTypeKey:       "grpc",
		config.TracingEndpointKey:           "127.0.0.1:4317",
		config.TracingInsecureKey:           "true",
		config.TracingSampleRateKey:         "1",
		config.TracingResourceAttributesKey: `{"gh_run_id":"7","network_uuid":"abc","node_id":"NodeID-1"}`,
	}, flags, "tracing flags")
}

func TestTracingFlagsIfAlloyRunningWithBadPIDFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	require.Nil(t, tracingFlagsIfAlloyRunning(logging.NoLog{}, nil), "no PID file")

	workingDir, err := getWorkingDir(alloyCmd)
	require.NoError(t, err, "getWorkingDir()")
	require.NoError(t, os.MkdirAll(workingDir, perms.ReadWriteExecute), "MkdirAll()")
	require.NoError(t, os.WriteFile(getPIDPath(workingDir), []byte("not-a-pid"), perms.ReadWrite), "WriteFile()")

	require.Nil(t, tracingFlagsIfAlloyRunning(logging.NoLog{}, nil), "garbage PID file")
}
