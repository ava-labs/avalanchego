// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"github.com/ava-labs/avalanchego/utils/logging"

	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
)

const (
	defaultPluginLogLevel  = logging.Info
	defaultPluginLogFormat = logging.Colors
)

// pluginLogLevel returns the most verbose level among the node's writers that
// display plugin logs, since plugin output is copied to each of them.
func pluginLogLevel(config logging.Config) logging.Level {
	if config.DisableWriterDisplaying {
		return config.LogLevel
	}
	return min(config.LogLevel, config.DisplayLevel)
}

func logLevelToProto(level logging.Level) vmpb.LogLevel {
	switch level {
	case logging.Verbo:
		return vmpb.LogLevel_LOG_LEVEL_VERBO
	case logging.Debug:
		return vmpb.LogLevel_LOG_LEVEL_DEBUG
	case logging.Trace:
		return vmpb.LogLevel_LOG_LEVEL_TRACE
	case logging.Info:
		return vmpb.LogLevel_LOG_LEVEL_INFO
	case logging.Warn:
		return vmpb.LogLevel_LOG_LEVEL_WARN
	case logging.Error:
		return vmpb.LogLevel_LOG_LEVEL_ERROR
	case logging.Fatal:
		return vmpb.LogLevel_LOG_LEVEL_FATAL
	case logging.Off:
		return vmpb.LogLevel_LOG_LEVEL_OFF
	default:
		return vmpb.LogLevel_LOG_LEVEL_UNSPECIFIED
	}
}

func logLevelFromProto(level vmpb.LogLevel) logging.Level {
	switch level {
	case vmpb.LogLevel_LOG_LEVEL_VERBO:
		return logging.Verbo
	case vmpb.LogLevel_LOG_LEVEL_DEBUG:
		return logging.Debug
	case vmpb.LogLevel_LOG_LEVEL_TRACE:
		return logging.Trace
	case vmpb.LogLevel_LOG_LEVEL_INFO:
		return logging.Info
	case vmpb.LogLevel_LOG_LEVEL_WARN:
		return logging.Warn
	case vmpb.LogLevel_LOG_LEVEL_ERROR:
		return logging.Error
	case vmpb.LogLevel_LOG_LEVEL_FATAL:
		return logging.Fatal
	case vmpb.LogLevel_LOG_LEVEL_OFF:
		return logging.Off
	default:
		return defaultPluginLogLevel
	}
}

// logFormatToProto does not map [logging.Auto], which must be resolved against
// the node's terminal before it is sent.
func logFormatToProto(format logging.Format) vmpb.LogFormat {
	switch format {
	case logging.Plain:
		return vmpb.LogFormat_LOG_FORMAT_PLAIN
	case logging.Colors:
		return vmpb.LogFormat_LOG_FORMAT_COLORS
	case logging.JSON:
		return vmpb.LogFormat_LOG_FORMAT_JSON
	default:
		return vmpb.LogFormat_LOG_FORMAT_UNSPECIFIED
	}
}

func logFormatFromProto(format vmpb.LogFormat) logging.Format {
	switch format {
	case vmpb.LogFormat_LOG_FORMAT_PLAIN:
		return logging.Plain
	case vmpb.LogFormat_LOG_FORMAT_COLORS:
		return logging.Colors
	case vmpb.LogFormat_LOG_FORMAT_JSON:
		return logging.JSON
	default:
		return defaultPluginLogFormat
	}
}
