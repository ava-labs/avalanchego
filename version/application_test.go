// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplicationString(t *testing.T) {
	tests := []struct {
		app      *Application
		expected string
	}{
		{
			app: &Application{
				Name:  Client,
				Major: 0,
				Minor: 0,
				Patch: 1,
			},
			expected: "avalanchego/0.0.1",
		},
		{
			app: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			expected: "avalanchego/1.2.3",
		},
		{
			app: &Application{
				Name:  "myClient",
				Major: 10,
				Minor: 20,
				Patch: 30,
			},
			expected: "myClient/10.20.30",
		},
	}

	for _, test := range tests {
		t.Run(test.expected, func(t *testing.T) {
			require.Equal(t, test.expected, test.app.String())
		})
	}
}

func TestApplicationSemantic(t *testing.T) {
	tests := []struct {
		app  *Application
		want string
	}{
		{
			app: &Application{
				Name:  Client,
				Major: 0,
				Minor: 0,
				Patch: 1,
			},
			want: "v0.0.1",
		},
		{
			app: &Application{
				Name:  Client,
				Major: 1,
				Minor: 14,
				Patch: 1,
			},
			want: "v1.14.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			require.Equal(t, tt.want, tt.app.Semantic())
		})
	}
}

func TestApplicationSemanticWithCommit(t *testing.T) {
	tests := []struct {
		name      string
		app       *Application
		gitCommit string
		want      string
	}{
		{
			name: "without commit",
			app: &Application{
				Name:  Client,
				Major: 1,
				Minor: 14,
				Patch: 1,
			},
			gitCommit: "",
			want:      "v1.14.1",
		},
		{
			name: "with commit",
			app: &Application{
				Name:  Client,
				Major: 1,
				Minor: 14,
				Patch: 1,
			},
			gitCommit: "abc123",
			want:      "v1.14.1@abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.app.SemanticWithCommit(tt.gitCommit)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestApplicationCompare(t *testing.T) {
	tests := []struct {
		myVersion   *Application
		peerVersion *Application
		expected    int
	}{
		{
			myVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			peerVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			expected: 0,
		},
		{
			myVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 4,
			},
			peerVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			expected: 1,
		},
		{
			myVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 3,
				Patch: 3,
			},
			peerVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			expected: 1,
		},
		{
			myVersion: &Application{
				Name:  Client,
				Major: 2,
				Minor: 2,
				Patch: 3,
			},
			peerVersion: &Application{
				Name:  Client,
				Major: 1,
				Minor: 2,
				Patch: 3,
			},
			expected: 1,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s_%s", test.myVersion, test.peerVersion), func(t *testing.T) {
			require := require.New(t)
			require.Equal(test.expected, test.myVersion.Compare(test.peerVersion))
			require.Equal(-test.expected, test.peerVersion.Compare(test.myVersion))
		})
	}
}
