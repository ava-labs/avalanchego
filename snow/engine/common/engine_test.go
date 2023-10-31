// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests the invariant that AppErrors are matched against their error codes
func TestAppError(t *testing.T) {
	tests := []struct {
		name     string
		err1     *AppError
		err2     error
		expected bool
	}{
		{
			name: "is - equal",
			err1: &AppError{
				Code:    1,
				Message: "foo",
			},
			err2: &AppError{
				Code:    1,
				Message: "foo",
			},
			expected: true,
		},
		{
			name: "is - same error code different messages",
			err1: &AppError{
				Code:    1,
				Message: "foo",
			},
			err2: &AppError{
				Code:    1,
				Message: "bar",
			},
			expected: true,
		},
		{
			name: "not is - different error code",
			err1: &AppError{
				Code: 1,
			},
			err2: &AppError{
				Code: 2,
			},
		},
		{
			name: "not is - different type",
			err1: &AppError{
				Code: 1,
			},
			err2: errors.New("foobar"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, errors.Is(tt.err1, tt.err2))
		})
	}
}
