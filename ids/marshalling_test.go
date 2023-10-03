// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func FuzzNodeIDMarshallUnmarshalInversion(f *testing.F) {
	f.Fuzz(func(t *testing.T, buf string) {
		var (
			require = require.New(t)
			input   = NodeID{buf: buf}
			output  = new(NodeID)
		)

		// json package marshalling
		b, err := json.Marshal(input)
		require.NoError(err)

		require.NoError(json.Unmarshal(b, output))
		require.Equal(input, *output)

		// MarshalJson/UnmarshalJson
		output = new(NodeID)
		b, err = input.MarshalJSON()
		require.NoError(err)

		require.NoError(output.UnmarshalJSON(b))
		require.Equal(input, *output)
	})
}

func FuzzShortNodeIDMarshallUnmarshalInversion(f *testing.F) {
	f.Fuzz(func(t *testing.T, buf []byte) {
		if len(buf) != ShortNodeIDLen {
			return
		}

		var (
			require = require.New(t)
			input   = ShortNodeID(buf)
			output  = new(ShortNodeID)
		)

		// json package marshalling
		b, err := json.Marshal(input)
		require.NoError(err)

		require.NoError(json.Unmarshal(b, output))
		require.Equal(input, *output)

		// MarshalJson/UnmarshalJson
		output = new(ShortNodeID)
		b, err = input.MarshalJSON()
		require.NoError(err)

		require.NoError(output.UnmarshalJSON(b))
		require.Equal(input, *output)
	})
}

func FuzzShortIDMarshallUnmarshalInversion(f *testing.F) {
	f.Fuzz(func(t *testing.T, buf []byte) {
		if len(buf) != ShortIDLen {
			return
		}

		var (
			require = require.New(t)
			input   = ShortID(buf)
			output  = new(ShortID)
		)

		// json package marshalling
		b, err := json.Marshal(input)
		require.NoError(err)

		require.NoError(json.Unmarshal(b, output))
		require.Equal(input, *output)

		// MarshalJson/UnmarshalJson
		output = new(ShortID)
		b, err = input.MarshalJSON()
		require.NoError(err)

		require.NoError(output.UnmarshalJSON(b))
		require.Equal(input, *output)
	})
}

func FuzzIDMarshallUnmarshalInversion(f *testing.F) {
	f.Fuzz(func(t *testing.T, buf []byte) {
		if len(buf) != IDLen {
			return
		}

		var (
			require = require.New(t)
			input   = ID(buf)
			output  = new(ID)
		)

		// json package marshalling
		b, err := json.Marshal(input)
		require.NoError(err)

		require.NoError(json.Unmarshal(b, output))
		require.Equal(input, *output)

		// MarshalJson/UnmarshalJson
		output = new(ID)
		b, err = input.MarshalJSON()
		require.NoError(err)

		require.NoError(output.UnmarshalJSON(b))
		require.Equal(input, *output)
	})
}
