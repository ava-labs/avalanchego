// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/profiler"

	"github.com/ava-labs/avalanchego/vms/sdk/context"
)

const (
	SnowVMConfigKey       = "snowvm"
	TracerConfigKey       = "tracer"
	ContinuousProfilerKey = "continuousProfiler"
)

type VMConfig struct {
	AcceptedBlockWindowCache int `json:"acceptedBlockWindowCache"`
}

func NewDefaultVMConfig() VMConfig {
	return VMConfig{
		AcceptedBlockWindowCache: 128,
	}
}

func GetVMConfig(config context.Config) (VMConfig, error) {
	return context.GetConfig(config, SnowVMConfigKey, NewDefaultVMConfig())
}

func GetProfilerConfig(config context.Config) (profiler.Config, error) {
	return context.GetConfig(config, ContinuousProfilerKey, profiler.Config{Enabled: false})
}

func GetTracerConfig(config context.Config) (trace.Config, error) {
	return context.GetConfig(config, TracerConfigKey, trace.Config{Enabled: false})
}
