// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"

	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
)

func TestLogLevelProtoRoundTrip(t *testing.T) {
	for _, level := range []logging.Level{
		logging.Verbo,
		logging.Debug,
		logging.Trace,
		logging.Info,
		logging.Warn,
		logging.Error,
		logging.Fatal,
		logging.Off,
	} {
		t.Run(level.String(), func(t *testing.T) {
			require := require.New(t)

			protoLevel := logLevelToProto(level)
			require.NotEqual(vmpb.LogLevel_LOG_LEVEL_UNSPECIFIED, protoLevel)
			require.Equal(level, logLevelFromProto(protoLevel))
		})
	}
}

func TestLogLevelToProtoUndefined(t *testing.T) {
	require.Equal(t, vmpb.LogLevel_LOG_LEVEL_UNSPECIFIED, logLevelToProto(logging.Level(0)))
}

func TestLogLevelFromProtoUnspecified(t *testing.T) {
	require.Equal(t, logging.Info, logLevelFromProto(vmpb.LogLevel_LOG_LEVEL_UNSPECIFIED))
}

func TestLogFormatProtoRoundTrip(t *testing.T) {
	for _, format := range []logging.Format{
		logging.Plain,
		logging.Colors,
		logging.JSON,
	} {
		protoFormat := logFormatToProto(format)
		require.NotEqual(t, vmpb.LogFormat_LOG_FORMAT_UNSPECIFIED, protoFormat)
		require.Equal(t, format, logFormatFromProto(protoFormat))
	}
}

func TestLogFormatToProtoAuto(t *testing.T) {
	require.Equal(t, vmpb.LogFormat_LOG_FORMAT_UNSPECIFIED, logFormatToProto(logging.Auto))
}

func TestLogFormatFromProtoUnspecified(t *testing.T) {
	require.Equal(t, logging.Colors, logFormatFromProto(vmpb.LogFormat_LOG_FORMAT_UNSPECIFIED))
}

func TestPluginLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		config   logging.Config
		expected logging.Level
	}{
		{
			name: "display more verbose",
			config: logging.Config{
				LogLevel:     logging.Info,
				DisplayLevel: logging.Debug,
			},
			expected: logging.Debug,
		},
		{
			name: "file more verbose",
			config: logging.Config{
				LogLevel:     logging.Debug,
				DisplayLevel: logging.Info,
			},
			expected: logging.Debug,
		},
		{
			name: "display disabled",
			config: logging.Config{
				DisableWriterDisplaying: true,
				LogLevel:                logging.Warn,
				DisplayLevel:            logging.Verbo,
			},
			expected: logging.Warn,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, pluginLogLevel(test.config))
		})
	}
}

type bufferCloser struct {
	bytes.Buffer
}

func (*bufferCloser) Close() error {
	return nil
}

func TestNewLogger(t *testing.T) {
	t.Run("debug level", func(t *testing.T) {
		require := require.New(t)

		var buf bufferCloser
		log := newLogger(ids.Empty, &vmpb.InitializeRequest{
			LogLevel:  vmpb.LogLevel_LOG_LEVEL_DEBUG,
			LogFormat: vmpb.LogFormat_LOG_FORMAT_PLAIN,
		}, &buf)
		log.Debug("debug line")

		require.Contains(buf.String(), "debug line")
	})

	t.Run("unspecified falls back to info and colors", func(t *testing.T) {
		require := require.New(t)

		var buf bufferCloser
		log := newLogger(ids.Empty, &vmpb.InitializeRequest{}, &buf)
		log.Debug("debug line")
		log.Info("info line")

		require.NotContains(buf.String(), "debug line")
		require.Contains(buf.String(), "info line")
		require.Contains(buf.String(), "\x1b[")
	})

	t.Run("json format", func(t *testing.T) {
		require := require.New(t)

		var buf bufferCloser
		log := newLogger(ids.Empty, &vmpb.InitializeRequest{
			LogFormat: vmpb.LogFormat_LOG_FORMAT_JSON,
		}, &buf)
		log.Info("info line")

		var line map[string]any
		require.NoError(json.Unmarshal(buf.Bytes(), &line))
		require.Equal("info line", line["msg"])
	})
}
