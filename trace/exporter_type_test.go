// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExporterTypeFromEnv(t *testing.T) {
	tests := []struct {
		name          string
		env           map[string]string
		expected      ExporterType
		expectedError error
	}{
		{
			name:     "unset_disables_tracing",
			expected: Disabled,
		},
		{
			name:     "none",
			env:      map[string]string{"OTEL_TRACES_EXPORTER": "none"},
			expected: Disabled,
		},
		{
			name:     "case_insensitive",
			env:      map[string]string{"OTEL_TRACES_EXPORTER": "NONE"},
			expected: Disabled,
		},
		{
			name: "otlp_defaults_to_http_protobuf",
			env:  map[string]string{"OTEL_TRACES_EXPORTER": "otlp"},
			// http/protobuf is the protocol the OTel spec recommends as the
			// default.
			expected: HTTP,
		},
		{
			name: "otlp_grpc",
			env: map[string]string{
				"OTEL_TRACES_EXPORTER":        "otlp",
				"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc",
			},
			expected: GRPC,
		},
		{
			name: "otlp_http_protobuf",
			env: map[string]string{
				"OTEL_TRACES_EXPORTER":        "otlp",
				"OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
			},
			expected: HTTP,
		},
		{
			name: "traces_protocol_overrides_general_protocol",
			env: map[string]string{
				"OTEL_TRACES_EXPORTER":               "otlp",
				"OTEL_EXPORTER_OTLP_PROTOCOL":        "http/protobuf",
				"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "grpc",
			},
			expected: GRPC,
		},
		{
			name: "unsupported_protocol",
			env: map[string]string{
				"OTEL_TRACES_EXPORTER":        "otlp",
				"OTEL_EXPORTER_OTLP_PROTOCOL": "http/json",
			},
			expectedError: errUnsupportedOTLPProtocol,
		},
		{
			name:          "unsupported_exporter",
			env:           map[string]string{"OTEL_TRACES_EXPORTER": "zipkin"},
			expectedError: errUnsupportedTracesExporter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			actual, err := ExporterTypeFromEnv()
			require.ErrorIs(t, err, tt.expectedError, "ExporterTypeFromEnv()")
			require.Equal(t, tt.expected, actual, "ExporterTypeFromEnv()")
		})
	}
}

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
