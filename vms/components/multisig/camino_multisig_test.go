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

func TestVerify(t *testing.T) {
	tests := map[string]struct {
		alias               Alias
		message             string
		expectedErrorString string
	}{
		"MemoSizeShouldBeLowerThanMaxMemoSize": {
			alias: Alias{
				Owners: &avax.TestVerifiable{},
				Memo:   make([]byte, avax.MaxMemoSize+1),
				ID:     hashing.ComputeHash160Array(ids.Empty[:]),
			},
			message:             "memo size should be lower than max memo size",
			expectedErrorString: "msig alias memo is larger (257 bytes) than max of 256 bytes",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.alias.Verify()
			if tt.expectedErrorString != "" {
				require.Error(t, err, tt.message)
				require.Equal(t, err.Error(), tt.expectedErrorString)
			} else {
				require.NoError(t, err, tt.message)
			}
		})
	}
}
