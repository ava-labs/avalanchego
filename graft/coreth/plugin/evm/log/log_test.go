// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package log

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrimPrefixes(t *testing.T) {
	tests := []struct {
		before string
		after  string
	}{
		{"", ""},
		{"/path/to/coreth/path/file.go", "path/file.go"},
		{"/path/to/coreth@version/path/file.go", "path/file.go"},
	}
	for _, test := range tests {
		require.Equal(t, test.after, trimPrefixes(test.before))
	}
}

func TestInitLogger(t *testing.T) {
	tests := []struct {
		logLevel    string
		expectedErr bool
	}{
		{
			logLevel: "info",
		},
		{
			logLevel:    "cchain", // invalid
			expectedErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.logLevel, func(t *testing.T) {
			require := require.New(t)
			_, err := InitLogger("alias", test.logLevel, true, os.Stderr)
			if test.expectedErr {
				require.ErrorContains(err, "unknown level") //nolint:forbidigo // uses upstream code
			} else {
				require.NoError(err)
			}
		})
	}
}
