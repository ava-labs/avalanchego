// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseDiffRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		want      diffRange
		wantError string
	}{
		{
			name:  "closed range",
			input: "origin/master..HEAD",
			want:  diffRange{baseRev: "origin/master", headRev: "HEAD", includeWorkingTree: false},
		},
		{
			name:  "open ended range includes working tree",
			input: "origin/master..",
			want:  diffRange{baseRev: "origin/master", includeWorkingTree: true},
		},
		{
			name:  "dotted ref name",
			input: "release/v1.2.3..HEAD",
			want:  diffRange{baseRev: "release/v1.2.3", headRev: "HEAD", includeWorkingTree: false},
		},
		{
			name:      "missing separator",
			input:     "origin/master",
			wantError: "must contain exactly one '..' separator",
		},
		{
			name:      "triple dot range unsupported",
			input:     "origin/master...HEAD",
			wantError: "must contain exactly one '..' separator",
		},
		{
			name:      "missing base revision",
			input:     "..HEAD",
			wantError: "base revision must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseDiffRange(tt.input)
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
