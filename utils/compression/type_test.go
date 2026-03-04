// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypeString(t *testing.T) {
	require := require.New(t)

	for _, compressionType := range []Type{TypeNone, TypeZstd} {
		s := compressionType.String()
		parsedType, err := TypeFromString(s)
		require.NoError(err)
		require.Equal(compressionType, parsedType)
	}

	_, err := TypeFromString("unknown")
	require.ErrorIs(err, errUnknownCompressionType)
}

func TestTypeMarshalJSON(t *testing.T) {
	type test struct {
		Type     Type
		expected string
	}

	tests := []test{
		{
			Type:     TypeNone,
			expected: `"none"`,
		},
		{
			Type:     TypeZstd,
			expected: `"zstd"`,
		},
		{
			Type:     Type(0),
			expected: `"unknown"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Type.String(), func(t *testing.T) {
			require := require.New(t)

			b, err := tt.Type.MarshalJSON()
			require.NoError(err)
			require.Equal(tt.expected, string(b))
		})
	}
}
