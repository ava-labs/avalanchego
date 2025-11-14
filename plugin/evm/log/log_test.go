// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package log

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

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
				require.ErrorContains(err, "unknown level")
			} else {
				require.NoError(err)
			}
		})
	}
}
