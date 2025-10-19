// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/require"
)

func TestDiscoverAvalanchegoCorethVersions(t *testing.T) {
	tests := []struct {
		name           string
		setupFiles     map[string]string // files to create (path -> content)
		requestedRepos []string
		explicitRefs   map[string]string
		expectedVers   map[string]string
		expectError    bool
	}{
		{
			name:           "both explicit",
			setupFiles:     nil, // no files needed
			requestedRepos: []string{"avalanchego", "coreth"},
			explicitRefs:   map[string]string{"avalanchego": "v1.11.0", "coreth": "v0.13.8"},
			expectedVers:   map[string]string{"avalanchego": "v1.11.0", "coreth": "v0.13.8"},
			expectError:    false,
		},
		{
			name: "avalanchego explicit, discover coreth from go.mod",
			setupFiles: map[string]string{
				"avalanchego/.git/HEAD": "ref: refs/heads/master",
				"avalanchego/go.mod": `module github.com/ava-labs/avalanchego

go 1.24

require github.com/ava-labs/coreth v0.13.8
`,
			},
			requestedRepos: []string{"avalanchego", "coreth"},
			explicitRefs:   map[string]string{"avalanchego": "v1.11.0"},
			expectedVers:   map[string]string{"avalanchego": "v1.11.0", "coreth": "v0.13.8"},
			expectError:    false,
		},
		{
			name: "coreth explicit, discover avalanchego from go.mod",
			setupFiles: map[string]string{
				"coreth/.git/HEAD": "ref: refs/heads/main",
				"coreth/go.mod": `module github.com/ava-labs/coreth

go 1.24

require github.com/ava-labs/avalanchego v1.11.0
`,
			},
			requestedRepos: []string{"avalanchego", "coreth"},
			explicitRefs:   map[string]string{"coreth": "v0.13.8"},
			expectedVers:   map[string]string{"avalanchego": "v1.11.0", "coreth": "v0.13.8"},
			expectError:    false,
		},
		{
			name: "avalanchego cloned, discover coreth",
			setupFiles: map[string]string{
				"avalanchego/.git/HEAD": "ref: refs/heads/master",
				"avalanchego/go.mod": `module github.com/ava-labs/avalanchego

go 1.24

require github.com/ava-labs/coreth v0.13.8
`,
			},
			requestedRepos: []string{"coreth"},
			explicitRefs:   map[string]string{},
			expectedVers:   map[string]string{"coreth": "v0.13.8"},
			expectError:    false,
		},
		{
			name: "coreth cloned, discover avalanchego",
			setupFiles: map[string]string{
				"coreth/.git/HEAD": "ref: refs/heads/main",
				"coreth/go.mod": `module github.com/ava-labs/coreth

go 1.24

require github.com/ava-labs/avalanchego v1.11.0
`,
			},
			requestedRepos: []string{"avalanchego"},
			explicitRefs:   map[string]string{},
			expectedVers:   map[string]string{"avalanchego": "v1.11.0"},
			expectError:    false,
		},
		{
			name:           "neither explicit, default avalanchego to master",
			setupFiles:     nil,
			requestedRepos: []string{"avalanchego", "coreth"},
			explicitRefs:   map[string]string{},
			expectedVers:   map[string]string{"avalanchego": "master"},
			expectError:    false,
		},
		{
			name:           "standalone mode - no files",
			setupFiles:     nil,
			requestedRepos: []string{"avalanchego", "coreth"},
			explicitRefs:   map[string]string{},
			expectedVers:   map[string]string{"avalanchego": "master"},
			expectError:    false,
		},
		{
			name: "only avalanchego requested, no explicit ref",
			setupFiles: map[string]string{
				"avalanchego/.git/HEAD": "ref: refs/heads/master",
			},
			requestedRepos: []string{"avalanchego"},
			explicitRefs:   map[string]string{},
			expectedVers:   map[string]string{"avalanchego": "master"},
			expectError:    false,
		},
		{
			name:           "only coreth requested, no explicit ref",
			setupFiles:     nil,
			requestedRepos: []string{"coreth"},
			explicitRefs:   map[string]string{},
			expectedVers:   map[string]string{"coreth": "master"}, // Note: coreth config has master as default branch
			expectError:    false,
		},
		{
			name: "pseudo-version in go.mod",
			setupFiles: map[string]string{
				"avalanchego/.git/HEAD": "ref: refs/heads/master",
				"avalanchego/go.mod": `module github.com/ava-labs/avalanchego

go 1.24

require github.com/ava-labs/coreth v0.13.8-0.20251007213349-63cc1a166a56
`,
			},
			requestedRepos: []string{"avalanchego", "coreth"},
			explicitRefs:   map[string]string{"avalanchego": "v1.11.0"},
			expectedVers:   map[string]string{"avalanchego": "v1.11.0", "coreth": "63cc1a166a56"},
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := logging.NoLog{}
			tmpDir := t.TempDir()

			// Create mock files
			for path, content := range tt.setupFiles {
				fullPath := filepath.Join(tmpDir, path)
				err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
				require.NoError(t, err)
				err = os.WriteFile(fullPath, []byte(content), 0o644)
				require.NoError(t, err)
			}

			// Call function
			versions, err := DiscoverAvalanchegoCorethVersions(log, tmpDir, tt.requestedRepos, tt.explicitRefs)

			// Validate
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				// Only check expected versions (function may return more if it clones repos)
				for repo, expectedVer := range tt.expectedVers {
					require.Contains(t, versions, repo)
					require.Equal(t, expectedVer, versions[repo], "version mismatch for %s", repo)
				}
			}
		})
	}
}

func TestDiscoverFirewoodVersion(t *testing.T) {
	tests := []struct {
		name               string
		setupFiles         map[string]string // files to create (path -> content)
		explicitRef        string
		requestedRepos     []string
		discoveredVersions map[string]string
		expectedVersion    string
		expectError        bool
	}{
		{
			name:               "explicit version",
			setupFiles:         nil,
			explicitRef:        "v0.2.0",
			requestedRepos:     []string{"firewood"},
			discoveredVersions: map[string]string{},
			expectedVersion:    "v0.2.0",
			expectError:        false,
		},
		{
			name: "from coreth - direct dependency",
			setupFiles: map[string]string{
				"coreth/.git/HEAD": "ref: refs/heads/main",
				"coreth/go.mod": `module github.com/ava-labs/coreth

go 1.24

require github.com/ava-labs/firewood-go-ethhash/ffi v0.2.0
`,
			},
			explicitRef:        "",
			requestedRepos:     []string{"coreth", "firewood"},
			discoveredVersions: map[string]string{"coreth": "main"},
			expectedVersion:    "v0.2.0",
			expectError:        false,
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
			explicitRef:        "",
			requestedRepos:     []string{"avalanchego", "firewood"},
			discoveredVersions: map[string]string{"avalanchego": "master"},
			expectedVersion:    "v0.2.0",
			expectError:        false,
		},
		{
			name:               "standalone - default branch",
			setupFiles:         nil,
			explicitRef:        "",
			requestedRepos:     []string{"firewood"},
			discoveredVersions: map[string]string{},
			expectedVersion:    "main",
			expectError:        false,
		},
		{
			name: "coreth already cloned",
			setupFiles: map[string]string{
				"coreth/.git/HEAD": "ref: refs/heads/main",
				"coreth/go.mod": `module github.com/ava-labs/coreth

go 1.24

require github.com/ava-labs/firewood-go-ethhash/ffi v0.2.0
`,
			},
			explicitRef:        "",
			requestedRepos:     []string{"firewood"}, // coreth not requested, but already cloned
			discoveredVersions: map[string]string{},
			expectedVersion:    "v0.2.0",
			expectError:        false,
		},
		{
			name: "avalanchego already cloned, no coreth",
			setupFiles: map[string]string{
				"avalanchego/.git/HEAD": "ref: refs/heads/master",
				"avalanchego/go.mod": `module github.com/ava-labs/avalanchego

go 1.24

require github.com/ava-labs/firewood-go-ethhash/ffi v0.2.0 // indirect
`,
			},
			explicitRef:        "",
			requestedRepos:     []string{"firewood"}, // avalanchego not requested, but already cloned
			discoveredVersions: map[string]string{},
			expectedVersion:    "v0.2.0",
			expectError:        false,
		},
		{
			name: "pseudo-version from coreth",
			setupFiles: map[string]string{
				"coreth/.git/HEAD": "ref: refs/heads/main",
				"coreth/go.mod": `module github.com/ava-labs/coreth

go 1.24

require github.com/ava-labs/firewood-go-ethhash/ffi v0.2.0-0.20251007213349-63cc1a166a56
`,
			},
			explicitRef:        "",
			requestedRepos:     []string{"coreth", "firewood"},
			discoveredVersions: map[string]string{"coreth": "main"},
			expectedVersion:    "63cc1a166a56",
			expectError:        false,
		},
		{
			name: "coreth involved but missing dependency",
			setupFiles: map[string]string{
				"coreth/.git/HEAD": "ref: refs/heads/main",
				"coreth/go.mod": `module github.com/ava-labs/coreth

go 1.24
`,
			},
			explicitRef:        "",
			requestedRepos:     []string{"coreth", "firewood"},
			discoveredVersions: map[string]string{"coreth": "main"},
			expectedVersion:    "",
			expectError:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := logging.NoLog{}
			tmpDir := t.TempDir()

			// Create mock files
			for path, content := range tt.setupFiles {
				fullPath := filepath.Join(tmpDir, path)
				err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
				require.NoError(t, err)
				err = os.WriteFile(fullPath, []byte(content), 0o644)
				require.NoError(t, err)
			}

			// Call function
			version, err := DiscoverFirewoodVersion(log, tmpDir, tt.explicitRef, tt.requestedRepos, tt.discoveredVersions)

			// Validate
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedVersion, version)
			}
		})
	}
}
