// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package multisig

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/stretchr/testify/require"
)

var _ Owners = (*testOwners)(nil)

type testOwners struct {
	avax.TestVerifiable
	isEmpty bool
}

func (o testOwners) IsZero() bool { return o.isEmpty }

func TestVerify(t *testing.T) {
	tests := map[string]struct {
		alias       Alias
		expectedErr error
	}{
		"Memo size should be lower than maxMemoSize": {
			alias: Alias{
				Owners: &testOwners{},
				Memo:   make([]byte, avax.MaxMemoSize+1),
				ID:     hashing.ComputeHash160Array(ids.Empty[:]),
			},
			expectedErr: errMemoIsTooBig,
		},
		"Zero owners": {
			alias: Alias{
				ID:     ids.ShortEmpty,
				Owners: &testOwners{isEmpty: true},
			},
			expectedErr: errEmptyAlias,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.alias.Verify()
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
