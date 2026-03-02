// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

import (
	"flag"

	"github.com/spf13/cast"
	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

type CollectorVars struct {
	StartMetricsCollector bool
	StartLogsCollector    bool
}

// NewCollectorFlagVars registers collector flag variables for stdlib flag
func NewCollectorFlagVars() *CollectorVars {
	v := &CollectorVars{}
	v.register(flag.BoolVar)
	return v
}

// NewCollectorFlagSetVars registers collector flag variables for pflag
func NewCollectorFlagSetVars(flagSet *pflag.FlagSet) *CollectorVars {
	v := &CollectorVars{}
	v.register(flagSet.BoolVar)
	return v
}

func (v *CollectorVars) register(boolVar varFunc[bool]) {
	boolVar(
		&v.StartMetricsCollector,
		"start-metrics-collector",
		cast.ToBool(tmpnet.GetEnvWithDefault("TMPNET_START_METRICS_COLLECTOR", "false")),
		"[optional] whether to start a local collector of metrics from nodes of the temporary network.",
	)

	boolVar(
		&v.StartLogsCollector,
		"start-logs-collector",
		cast.ToBool(tmpnet.GetEnvWithDefault("TMPNET_START_LOGS_COLLECTOR", "false")),
		"[optional] whether to start a local collector of logs from nodes of the temporary network.",
	)
}
