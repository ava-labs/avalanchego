// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJSON(t *testing.T) {
	tests := []struct {
		name         string
		value        JSONByteSlice
		expectedJSON string
	}{
		{
			name:         "nil",
			value:        nil,
			expectedJSON: nullStr,
		},
		{
			name:         "empty",
			value:        []byte{},
			expectedJSON: `"0x"`,
		},
		{
			name:         "not empty",
			value:        []byte{0, 1, 2, 3},
			expectedJSON: `"0x00010203"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			jsonBytes, err := json.Marshal(test.value)
			require.NoError(err)
			require.JSONEq(test.expectedJSON, string(jsonBytes))

			var unmarshaled JSONByteSlice
			require.NoError(json.Unmarshal(jsonBytes, &unmarshaled))
			require.Equal(test.value, unmarshaled)
		})
	}
}

func TestUnmarshalJSONNullKeepsInitialValue(t *testing.T) {
	require := require.New(t)

	unmarshaled := JSONByteSlice{1, 2, 3}
	require.NoError(json.Unmarshal([]byte(nullStr), &unmarshaled))
	require.Equal(JSONByteSlice{1, 2, 3}, unmarshaled)
}
