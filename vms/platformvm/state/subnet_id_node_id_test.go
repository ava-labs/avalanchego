// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thepudds/fzgen/fuzzer"
)

func FuzzSubnetIDNodeIDMarshal(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)

		var v subnetIDNodeID
		fz := fuzzer.NewFuzzer(data)
		fz.Fill(&v)

		marshalledData := v.Marshal()

		var parsed subnetIDNodeID
		require.NoError(parsed.Unmarshal(marshalledData))
		require.Equal(v, parsed)
	})
}

func FuzzSubnetIDNodeIDUnmarshal(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)

		var v subnetIDNodeID
		if err := v.Unmarshal(data); err != nil {
			require.ErrorIs(err, errUnexpectedSubnetIDNodeIDLength)
			return
		}

		marshalledData := v.Marshal()
		require.Equal(data, marshalledData)
	})
}

func FuzzSubnetIDNodeIDOrdering(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		var (
			v0 subnetIDNodeID
			v1 subnetIDNodeID
		)
		fz := fuzzer.NewFuzzer(data)
		fz.Fill(&v0, &v1)

		if v0.subnetID == v1.subnetID {
			return
		}

		key0 := v0.Marshal()
		key1 := v1.Marshal()
		require.Equal(
			t,
			v0.subnetID.Compare(v1.subnetID),
			bytes.Compare(key0, key1),
		)
	})
}
