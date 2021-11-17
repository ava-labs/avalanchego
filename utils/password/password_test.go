// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package password

import (
	"fmt"
	"testing"
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
			if !SufficientlyStrong(test.password, test.expected) {
				t.Fatalf("expected %q to be rated stronger", test.password)
			}
		})
	}
}

func TestIsValid(t *testing.T) {
	tests := []struct {
		password  string
		expected  Strength
		shouldErr bool
	}{
		{
			password:  "",
			expected:  VeryWeak,
			shouldErr: true,
		},
		{
			password:  "a",
			expected:  VeryWeak,
			shouldErr: false,
		},
		{
			password:  "password",
			expected:  VeryWeak,
			shouldErr: false,
		},
		{
			password:  "thisisareallylongandpresumablyverystrongpassword",
			expected:  VeryStrong,
			shouldErr: false,
		},
		{
			password:  string(make([]byte, maxPassLen)),
			expected:  VeryWeak,
			shouldErr: false,
		},
		{
			password:  string(make([]byte, maxPassLen+1)),
			expected:  VeryWeak,
			shouldErr: true,
		},
		{
			password:  "password",
			expected:  Weak,
			shouldErr: true,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s-%d", test.password, test.expected), func(t *testing.T) {
			err := IsValid(test.password, test.expected)
			if err == nil && test.shouldErr {
				t.Fatalf("expected %q to be invalid", test.password)
			}
			if err != nil && !test.shouldErr {
				t.Fatalf("expected %q to be valid but returned %s", test.password, err)
			}
		})
	}
}
