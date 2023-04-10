// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	v, err := Parse("v1.2.3")

	require.NoError(t, err)
	require.NotNil(t, v)
	require.Equal(t, "v1.2.3", v.String())
	require.Equal(t, 1, v.Major)
	require.Equal(t, 2, v.Minor)
	require.Equal(t, 3, v.Patch)

	badVersions := []string{
		"",
		"1.2.3",
		"vz.2.3",
		"v1.z.3",
		"v1.2.z",
	}
	for _, badVersion := range badVersions {
		_, err := Parse(badVersion)
		require.Error(t, err)
	}
}

func TestParseApplication(t *testing.T) {
	v, err := ParseApplication("avalanche/1.2.3")

	require.NoError(t, err)
	require.NotNil(t, v)
	require.Equal(t, "avalanche/1.2.3", v.String())
	require.Equal(t, 1, v.Major)
	require.Equal(t, 2, v.Minor)
	require.Equal(t, 3, v.Patch)
	require.NoError(t, v.Compatible(v))
	require.False(t, v.Before(v))

	badVersions := []string{
		"",
		"avalanche/",
		"avalanche/z.0.0",
		"avalanche/0.z.0",
		"avalanche/0.0.z",
	}
	for _, badVersion := range badVersions {
		_, err := ParseApplication(badVersion)
		require.Error(t, err)
	}
}
