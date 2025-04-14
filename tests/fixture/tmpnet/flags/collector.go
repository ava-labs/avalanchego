// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

import (
	"flag"

	"github.com/spf13/cast"
	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

type CollectorVars struct {
	StartCollectors bool
}

// NewCollectorsFlagVars registers collector flag variables for stdlib flag
func NewCollectorsFlagVars() *CollectorVars {
	v := &CollectorVars{}
	v.register(flag.BoolVar)
	return v
}

// NewRuntimeConfigFlagSetVars registers collector flag variables for pflag
func NewCollectorFlagSetVars(flagSet *pflag.FlagSet) *CollectorVars {
	v := &CollectorVars{}
	v.register(flagSet.BoolVar)
	return v
}

func (v *CollectorVars) register(boolVar varFunc[bool]) {
	boolVar(
		&v.StartCollectors,
		"start-collectors",
		cast.ToBool(tmpnet.GetEnvWithDefault("TMPNET_START_COLLECTORS", "false")),
		"Whether to start collectors of logs and metrics from nodes of the temporary network.",
	)
}
