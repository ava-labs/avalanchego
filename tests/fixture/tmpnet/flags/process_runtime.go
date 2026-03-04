// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const (
	processRuntime   = "process"
	processDocPrefix = "[process runtime] "

	avalanchegoPathFlag = "avalanchego-path"
)

var errAvalancheGoRequired = fmt.Errorf("--%s or %s are required", avalanchegoPathFlag, tmpnet.AvalancheGoPathEnvName)

type processRuntimeVars struct {
	config tmpnet.ProcessRuntimeConfig
}

func (v *processRuntimeVars) registerWithFlag() {
	v.register(flag.StringVar, flag.BoolVar)
}

func (v *processRuntimeVars) registerWithFlagSet(flagSet *pflag.FlagSet) {
	v.register(flagSet.StringVar, flagSet.BoolVar)
}

func (v *processRuntimeVars) register(stringVar varFunc[string], boolVar varFunc[bool]) {
	stringVar(
		&v.config.AvalancheGoPath,
		avalanchegoPathFlag,
		os.Getenv(tmpnet.AvalancheGoPathEnvName),
		processDocPrefix+fmt.Sprintf(
			"The avalanchego executable path. Also possible to configure via the %s env variable.",
			tmpnet.AvalancheGoPathEnvName,
		),
	)
	stringVar(
		&v.config.PluginDir,
		"plugin-dir",
		tmpnet.GetEnvWithDefault(tmpnet.AvalancheGoPluginDirEnvName, os.ExpandEnv("$HOME/.avalanchego/plugins")),
		processDocPrefix+fmt.Sprintf(
			"The dir containing VM plugins. Also possible to configure via the %s env variable.",
			tmpnet.AvalancheGoPluginDirEnvName,
		),
	)
	boolVar(
		&v.config.ReuseDynamicPorts,
		"reuse-dynamic-ports",
		false,
		processDocPrefix+"Whether to attempt to reuse dynamically allocated ports across node restarts.",
	)
}

func (v *processRuntimeVars) getProcessRuntimeConfig() (*tmpnet.ProcessRuntimeConfig, error) {
	if err := v.validate(); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return &v.config, nil
}

func (v *processRuntimeVars) validate() error {
	path := v.config.AvalancheGoPath

	if len(path) == 0 {
		return stacktrace.Wrap(errAvalancheGoRequired)
	}

	if filepath.IsAbs(path) {
		if _, err := os.Stat(path); err != nil {
			return stacktrace.Errorf("--%s (%s) not found: %w", avalanchegoPathFlag, path, err)
		}
		return nil
	}

	// A relative path must be resolvable to an absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return stacktrace.Errorf(
			"--%s (%s) is a relative path but its absolute path cannot be determined: %w",
			avalanchegoPathFlag,
			path,
			err,
		)
	}

	// The absolute path must exist
	if _, err := os.Stat(absPath); err != nil {
		return stacktrace.Errorf(
			"--%s (%s) is a relative path but its absolute path (%s) is not found: %w",
			avalanchegoPathFlag,
			path,
			absPath,
			err,
		)
	}
	return nil
}
