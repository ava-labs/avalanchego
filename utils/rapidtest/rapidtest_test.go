// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rapidtest

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaybeSetRapidSeed(t *testing.T) {
	const (
		seed    = "-" + seedFlag
		shuffle = "-" + shuffleFlag
	)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "defaults",
			args: nil,
			want: FixedSeed,
		},
		{
			name: "explicit_seed",
			args: []string{seed, "99"},
			want: "99",
		},
		{
			name: "explicit_seed_equal_to_default",
			args: []string{seed, ""},
			want: "",
		},
		{
			name: "explicit_shuffle_off",
			args: []string{shuffle, "off"},
			want: FixedSeed,
		},
		{
			name: "shuffle_on",
			args: []string{shuffle, "on"},
			want: "",
		},
		{
			name: "shuffle_seed",
			args: []string{shuffle, "12345"},
			want: "",
		},
		{
			name: "explicit_seed_and_shuffle_on",
			args: []string{seed, "99", shuffle, "on"},
			want: "99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("", flag.PanicOnError)
			fs.String(shuffleFlag, "off", "")
			got := fs.String(seedFlag, "", "")

			require.NoErrorf(t, fs.Parse(tt.args), "%T.Parse(%q)", fs, tt.args)
			require.NoError(t, maybeSetRapidSeed(fs), "maybeSetRapidSeed()")
			require.Equalf(t, tt.want, *got, "%q flag value", seedFlag)
		})
	}
}
