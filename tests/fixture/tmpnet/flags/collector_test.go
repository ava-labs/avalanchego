// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestCollectorFlagVarsTraces(t *testing.T) {
	t.Run("flag", func(t *testing.T) {
		flagSet := pflag.NewFlagSet("test", pflag.ContinueOnError)
		v := NewCollectorFlagSetVars(flagSet)
		require.NoError(t, flagSet.Parse([]string{"--start-traces-collector=true"}), "flagSet.Parse()")
		require.True(t, v.StartTracesCollector, "StartTracesCollector from flag")
	})

	t.Run("env", func(t *testing.T) {
		t.Setenv("TMPNET_START_TRACES_COLLECTOR", "true")
		flagSet := pflag.NewFlagSet("test", pflag.ContinueOnError)
		v := NewCollectorFlagSetVars(flagSet)
		require.NoError(t, flagSet.Parse(nil), "flagSet.Parse()")
		require.True(t, v.StartTracesCollector, "StartTracesCollector from env")
	})
}
