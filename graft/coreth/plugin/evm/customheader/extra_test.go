// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
)

func TestMain(m *testing.M) {
	customtypes.Register()
	os.Exit(m.Run())
}

func TestPredicateBytesFromExtra(t *testing.T) {
	tests := []struct {
		name     string
		rules    extras.AvalancheRules
		extra    []byte
		expected []byte
	}{
		{
			name:     "empty_extra",
			extra:    nil,
			expected: nil,
		},
		{
			name:     "too_short",
			extra:    make([]byte, ap3.WindowSize-1),
			expected: nil,
		},
		{
			name:     "empty_predicate",
			extra:    make([]byte, ap3.WindowSize),
			expected: nil,
		},
		{
			name: "non_empty_predicate",
			extra: []byte{
				ap3.WindowSize: 5,
			},
			expected: []byte{5},
		},
		{
			name: "fortuna_empty_extra",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra:    nil,
			expected: nil,
		},
		{
			name: "fortuna_too_short",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176.StateSize-1),
			expected: nil,
		},
		{
			name: "fortuna_empty_predicate",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra:    make([]byte, acp176.StateSize),
			expected: nil,
		},
		{
			name: "fortuna_non_empty_predicate",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra: []byte{
				acp176.StateSize: 5,
			},
			expected: []byte{5},
		},
		{
			name: "helicon_empty_predicate",
			rules: extras.AvalancheRules{
				IsHelicon: true,
			},
			extra:    nil,
			expected: nil,
		},
		{
			name: "helicon_non_empty_predicate",
			rules: extras.AvalancheRules{
				IsHelicon: true,
			},
			extra:    []byte{5},
			expected: []byte{5},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := PredicateBytesFromExtra(test.rules, test.extra)
			require.Equal(t, test.expected, got)
		})
	}
}
