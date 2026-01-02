// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshal(t *testing.T) {
	tests := []struct {
		name          string
		exporter      ExporterType
		expected      string
		expectedError error
	}{
		{
			name:          "unknown_type",
			exporter:      255,
			expectedError: errUnknownExporterType,
		},
		{
			name:     "disabled",
			exporter: Disabled,
			expected: `"disabled"`,
		},
		{
			name:     "grpc",
			exporter: GRPC,
			expected: `"grpc"`,
		},
		{
			name:     "http",
			exporter: HTTP,
			expected: `"http"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := tt.exporter.MarshalJSON()
			require.ErrorIs(err, tt.expectedError)
			require.Equal(tt.expected, string(actual))
		})
	}
}

func TestUnmarshal(t *testing.T) {
	tests := []struct {
		name          string
		json          string
		expected      ExporterType
		expectedError error
	}{
		{
			name:          "no_quotes",
			json:          "grpc",
			expectedError: errMissingQuotes,
		},
		{
			name:          "single_left_quote",
			json:          `"grpc`,
			expectedError: errMissingQuotes,
		},
		{
			name:          "single_right_quote",
			json:          `grpc"`,
			expectedError: errMissingQuotes,
		},
		{
			name:          "only_one_quote",
			json:          `"`,
			expectedError: errMissingQuotes,
		},
		{
			name:          "multiple_quotes",
			json:          `""grpc"""`,
			expectedError: errUnknownExporterType,
		},
		{
			name:          "empty_string",
			json:          `""`,
			expectedError: errUnknownExporterType,
		},
		{
			name: "null",
			json: `null`,
		},
		{
			name:     "disabled",
			json:     `"disabled"`,
			expected: Disabled,
		},
		{
			name:     "grpc",
			json:     `"grpc"`,
			expected: GRPC,
		},
		{
			name:     "http",
			json:     `"http"`,
			expected: HTTP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			var actual ExporterType
			err := actual.UnmarshalJSON([]byte(tt.json))
			require.ErrorIs(err, tt.expectedError)
			require.Equal(tt.expected, actual)
		})
	}
}
