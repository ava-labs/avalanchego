// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	require := require.New(t)

	v, err := Parse("v1.2.3")

	require.NoError(err)
	require.NotNil(v)
	require.Equal("v1.2.3", v.String())
	require.Equal(1, v.Major)
	require.Equal(2, v.Minor)
	require.Equal(3, v.Patch)

	tests := []struct {
		version     string
		expectedErr error
	}{
		{
			version:     "",
			expectedErr: errMissingVersionPrefix,
		},
		{
			version:     "1.2.3",
			expectedErr: errMissingVersionPrefix,
		},
		{
			version:     "z1.2.3",
			expectedErr: errMissingVersionPrefix,
		},
		{
			version:     "v1.2",
			expectedErr: errMissingVersions,
		},
		{
			version:     "vz.2.3",
			expectedErr: strconv.ErrSyntax,
		},
		{
			version:     "v1.z.3",
			expectedErr: strconv.ErrSyntax,
		},
		{
			version:     "v1.2.z",
			expectedErr: strconv.ErrSyntax,
		},
		{
			version:     "v1.2.3.4",
			expectedErr: strconv.ErrSyntax,
		},
	}
	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			_, err := Parse(test.version)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}

func TestParseApplication(t *testing.T) {
	require := require.New(t)

	v, err := ParseApplication("avalanche/1.2.3")

	require.NoError(err)
	require.NotNil(v)
	require.Equal("avalanche/1.2.3", v.String())
	require.Equal(1, v.Major)
	require.Equal(2, v.Minor)
	require.Equal(3, v.Patch)
	require.NoError(v.Compatible(v))
	require.False(v.Before(v))

	tests := []struct {
		version     string
		expectedErr error
	}{
		{
			version:     "",
			expectedErr: errMissingApplicationPrefix,
		},
		{
			version:     "avalanche/",
			expectedErr: errMissingVersions,
		},
		{
			version:     "avalanche/z.0.0",
			expectedErr: strconv.ErrSyntax,
		},
		{
			version:     "avalanche/0.z.0",
			expectedErr: strconv.ErrSyntax,
		},
		{
			version:     "avalanche/0.0.z",
			expectedErr: strconv.ErrSyntax,
		},
		{
			version:     "avalanche/0.0.0.0",
			expectedErr: strconv.ErrSyntax,
		},
	}
	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			_, err := ParseApplication(test.version)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}
