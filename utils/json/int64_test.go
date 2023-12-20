package json

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInt64(t *testing.T) {
	type test struct {
		i           Int64
		expectedStr string
	}

	tests := []test{
		{
			i:           0,
			expectedStr: `"0"`,
		},
		{
			i:           1,
			expectedStr: `"1"`,
		},
		{
			i:           100,
			expectedStr: `"100"`,
		},
		{
			i:           -1,
			expectedStr: `"-1"`,
		},
		{
			i:           math.MaxInt64,
			expectedStr: `"9223372036854775807"`,
		},
		{
			i:           math.MinInt64,
			expectedStr: `"-9223372036854775808"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.expectedStr, func(t *testing.T) {
			require := require.New(t)

			jsonBytes, err := tt.i.MarshalJSON()
			require.NoError(err)

			require.Equal(tt.expectedStr, string(jsonBytes))

			var i Int64
			require.NoError(i.UnmarshalJSON(jsonBytes))
			require.Equal(tt.i, i)
		})
	}

	t.Run("null", func(t *testing.T) {
		require := require.New(t)

		var i Int64
		require.NoError(i.UnmarshalJSON([]byte(Null)))
		require.Equal(Int64(0), i)
	})

	t.Run("invalid", func(t *testing.T) {
		require := require.New(t)

		var i Int64
		require.Error(i.UnmarshalJSON([]byte("wrong")))
	})
}
