// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/log"
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
	require := require.New(t)
	_, err := InitLogger("alias", "info", true, os.Stderr)
	require.NoError(err)
	log.Info("test")
}
