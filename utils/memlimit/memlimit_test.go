// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package memlimit

import (
	"math"
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestSet(t *testing.T) {
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })

	tests := []struct {
		name    string
		ratio   float64
		env     string
		wantErr error
		changed bool
	}{
		{name: "disabled", ratio: 0},
		{name: "negative", ratio: -0.1, wantErr: ErrInvalidRatio},
		{name: "above one", ratio: 1.1, wantErr: ErrInvalidRatio},
		{name: "env override", ratio: 0.5, env: "1GiB"},
		{name: "default", ratio: DefaultRatio, changed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			debug.SetMemoryLimit(math.MaxInt64)
			if tt.env != "" {
				t.Setenv("GOMEMLIMIT", tt.env)
			}

			err := Set(tt.ratio, logging.NoLog{})
			require.ErrorIs(t, err, tt.wantErr, "Set()")

			limit := debug.SetMemoryLimit(-1)
			if tt.changed {
				require.Less(t, limit, int64(math.MaxInt64), "memory limit unchanged")
				require.Positive(t, limit, "memory limit")
			} else {
				require.Equal(t, int64(math.MaxInt64), limit, "memory limit changed")
			}
		})
	}
}
