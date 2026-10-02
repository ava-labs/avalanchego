// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"encoding/json"
	"testing"

	"github.com/ava-labs/libevm/libevm"
	"github.com/stretchr/testify/require"
)

// heliconJSON encodes only [NetworkUpgrades.HeliconTimestamp], as a bare
// number.
type heliconJSON struct{}

func (heliconJSON) Marshal(n *NetworkUpgrades) ([]byte, error) {
	return json.Marshal(n.HeliconTimestamp)
}

func (heliconJSON) Unmarshal(data []byte, n *NetworkUpgrades) error {
	return json.Unmarshal(data, &n.HeliconTimestamp)
}

func TestNetworkUpgradesJSONUnregistered(t *testing.T) {
	_, err := json.Marshal(NetworkUpgrades{})
	require.ErrorIs(t, err, ErrNoJSONCodec)

	var n NetworkUpgrades
	require.ErrorIs(t, json.Unmarshal([]byte(`{}`), &n), ErrNoJSONCodec)
}

func TestWithTempRegisteredJSON(t *testing.T) {
	ts := uint64(42)
	want := NetworkUpgrades{HeliconTimestamp: &ts}

	err := libevm.WithTemporaryExtrasLock(func(lock libevm.ExtrasLock) error {
		return WithTempRegisteredJSON(lock, heliconJSON{}, func() error {
			got, err := json.Marshal(want)
			require.NoError(t, err)
			require.JSONEq(t, `42`, string(got))

			var n NetworkUpgrades
			require.NoError(t, json.Unmarshal(got, &n))
			require.Equal(t, want, n)
			return nil
		})
	})
	require.NoError(t, err)

	_, err = json.Marshal(want)
	require.ErrorIs(t, err, ErrNoJSONCodec, "codec not restored")
}

func TestRegisterJSONTwicePanics(t *testing.T) {
	t.Cleanup(func() { jsonCodec = nil })

	RegisterJSON(heliconJSON{})
	require.Panics(t, func() { RegisterJSON(heliconJSON{}) })
}
