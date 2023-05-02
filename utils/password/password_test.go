// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package password

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSufficientlyStrong(t *testing.T) {
	tests := []struct {
		password string
		expected Strength
	}{
		{
			password: "",
			expected: VeryWeak,
		},
		{
			password: "a",
			expected: VeryWeak,
		},
		{
			password: "password",
			expected: VeryWeak,
		},
		{
			password: "thisisareallylongandpresumablyverystrongpassword",
			expected: VeryStrong,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s-%d", test.password, test.expected), func(t *testing.T) {
			require := require.New(t)
			require.True(SufficientlyStrong(test.password, test.expected))
		})
	}
}

func TestIsValid(t *testing.T) {
	tests := []struct {
		password    string
		expected    Strength
		expectedErr error
	}{
		{
			password:    "",
			expected:    VeryWeak,
			expectedErr: ErrEmptyPassword,
		},
		{
			password: "a",
			expected: VeryWeak,
		},
		{
			password: "password",
			expected: VeryWeak,
		},
		{
			password: "thisisareallylongandpresumablyverystrongpassword",
			expected: VeryStrong,
		},
		{
			password: string(make([]byte, maxPassLen)),
			expected: VeryWeak,
		},
		{
			password:    string(make([]byte, maxPassLen+1)),
			expected:    VeryWeak,
			expectedErr: ErrPassMaxLength,
		},
		{
			password:    "password",
			expected:    Weak,
			expectedErr: errWeakPassword,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s-%d", test.password, test.expected), func(t *testing.T) {
			require := require.New(t)
			require.ErrorIs(IsValid(test.password, test.expected), test.expectedErr)
		})
	}
}
