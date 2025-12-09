// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests/fixture/polyrepo/internal/logging"
)

func TestDiscoverFirewoodVersion(t *testing.T) {
	tests := []struct {
		name            string
		setupFiles      map[string]string // files to create (path -> content)
		explicitRef     string
		expectedVersion string
		expectError     bool
	}{
		{
			name:            "explicit version",
			setupFiles:      nil,
			explicitRef:     "v0.2.0",
			expectedVersion: "v0.2.0",
			expectError:     false,
		},
		{
			name: "from grafted coreth - direct dependency",
			setupFiles: map[string]string{
				"avalanchego/.git/HEAD": "ref: refs/heads/master",
				"avalanchego/graft/coreth/go.mod": `module github.com/ava-labs/coreth

go 1.24

require github.com/ava-labs/firewood-go-ethhash/ffi v0.2.0
`,
			},
			explicitRef:     "",
			expectedVersion: "v0.2.0",
			expectError:     false,
		},
		{
			name: "from avalanchego - indirect dependency",
			setupFiles: map[string]string{
				"avalanchego/.git/HEAD": "ref: refs/heads/master",
				"avalanchego/go.mod": `module github.com/ava-labs/avalanchego

go 1.24

require github.com/ava-labs/firewood-go-ethhash/ffi v0.2.0 // indirect
`,
			},
			explicitRef:     "",
			expectedVersion: "v0.2.0",
			expectError:     false,
		},
		{
			name:            "standalone - default branch (no avalanchego cloned)",
			setupFiles:      nil,
			explicitRef:     "",
			expectedVersion: "main",
			expectError:     false,
		},
		{
			name: "avalanchego already cloned",
			setupFiles: map[string]string{
				"avalanchego/.git/HEAD": "ref: refs/heads/master",
				"avalanchego/go.mod": `module github.com/ava-labs/avalanchego

go 1.24

require github.com/ava-labs/firewood-go-ethhash/ffi v0.2.0 // indirect
`,
			},
			explicitRef:     "",
			expectedVersion: "v0.2.0",
			expectError:     false,
		},
		{
			name: "pseudo-version from avalanchego",
			setupFiles: map[string]string{
				"avalanchego/.git/HEAD": "ref: refs/heads/master",
				"avalanchego/go.mod": `module github.com/ava-labs/avalanchego

go 1.24

require github.com/ava-labs/firewood-go-ethhash/ffi v0.2.0-0.20251007213349-63cc1a166a56
`,
			},
			explicitRef:     "",
			expectedVersion: "63cc1a166a56",
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := logging.NoLog{}
			tmpDir := t.TempDir()

			// Create mock files
			for path, content := range tt.setupFiles {
				fullPath := filepath.Join(tmpDir, path)
				require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
				require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o600))
			}

			// Call function with new signature (no requestedRepos, no discoveredVersions)
			version, err := DiscoverFirewoodVersion(log, tmpDir, tt.explicitRef)

			// Validate
			if tt.expectError {
				require.Error(t, err) //nolint:forbidigo // checking that an error occurred without checking type
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedVersion, version)
			}
		})
	}
}
