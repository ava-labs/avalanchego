// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package json

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFloat32(t *testing.T) {
	require := require.New(t)

	type test struct {
		f                    Float32
		expectedStr          string
		expectedUnmarshalled float32
	}

	tests := []test{
		{
			0,
			"0.0000",
			0,
		}, {
			0.00001,
			"0.0000",
			0,
		}, {
			1,
			"1.0000",
			1,
		}, {
			1.0001,
			"1.0001",
			1.0001,
		}, {
			100.0000,
			"100.0000",
			100.0000,
		}, {
			100.0001,
			"100.0001",
			100.0001,
		},
	}

	for _, tt := range tests {
		jsonBytes, err := tt.f.MarshalJSON()
		require.NoError(err)
		require.JSONEq(fmt.Sprintf(`"%s"`, tt.expectedStr), string(jsonBytes))

		var f Float32
		require.NoError(f.UnmarshalJSON(jsonBytes))
		require.Equal(tt.expectedUnmarshalled, float32(f))
	}
}
