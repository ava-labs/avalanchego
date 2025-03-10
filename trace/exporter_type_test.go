// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		Name         string
		ExporterType ExporterType
		ExpectedErr  error
	}{
		{
			Name:         "GRPC",
			ExporterType: GRPC,
		},
		{
			Name:         "HTTP",
			ExporterType: HTTP,
		},
		{
			Name:         "NoOp",
			ExporterType: NoOp,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			b, err := json.Marshal(tt.ExporterType)
			require.NoError(err)

			var et ExporterType
			require.NoError(json.Unmarshal(b, &et))

			require.Equal(tt.ExporterType, et)
		})
	}
}

func TestUnmarshal(t *testing.T) {
	tests := []struct {
		Name          string
		Str           string
		ExpectedError error
	}{
		{
			Name:          "NoQuotes",
			Str:           "grpc",
			ExpectedError: errInvalidFormat,
		},
		{
			Name:          "SingleLeftQuote",
			Str:           "\"grpc",
			ExpectedError: errInvalidFormat,
		},
		{
			Name:          "SingleRightQuote",
			Str:           "grpc\"",
			ExpectedError: errInvalidFormat,
		},
		{
			Name:          "MultipleQuotes",
			Str:           "\"\"grpc\"\"\"",
			ExpectedError: errUnknownExporterType,
		},
		{
			Name:          "NullString",
			Str:           "\"null\"",
			ExpectedError: nil,
		},
		{
			Name:          "EmptyString",
			Str:           "\"\"",
			ExpectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			var et ExporterType

			err := et.UnmarshalJSON([]byte(tt.Str))
			require.ErrorIs(err, tt.ExpectedError)
		})
	}
}
