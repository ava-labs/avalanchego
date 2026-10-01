// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"encoding/json"
	"testing"

	"github.com/ava-labs/libevm/libevm"
	"github.com/stretchr/testify/require"

	evmparams "github.com/ava-labs/avalanchego/graft/evm/params"
)

func TestCorethJSON(t *testing.T) {
	ts := uint64(42)
	want := NetworkUpgrades{
		ApricotPhase1BlockTimestamp: &ts,
		DurangoBlockTimestamp:       &ts,
		HeliconTimestamp:            &ts,
		IglooTimestamp:              &ts,
	}
	const wantJSON = `{"apricotPhase1BlockTimestamp":42,"durangoBlockTimestamp":42,"heliconTimestamp":42,"iglooTimestamp":42}`

	err := libevm.WithTemporaryExtrasLock(func(lock libevm.ExtrasLock) error {
		return evmparams.WithTempRegisteredJSON(lock, CorethJSON{}, func() error {
			got, err := json.Marshal(want)
			require.NoError(t, err)
			require.JSONEq(t, wantJSON, string(got))

			var n NetworkUpgrades
			require.NoError(t, json.Unmarshal([]byte(wantJSON), &n))
			require.Equal(t, want, n)
			return nil
		})
	})
	require.NoError(t, err)
}
