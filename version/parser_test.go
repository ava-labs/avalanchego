// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"strconv"
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
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}
