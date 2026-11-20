// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigResolvePendingToLastExecuted(t *testing.T) {
	// This test only exercises plumbing of the config. A full test of
	// functionality is performed in the SAE code.

	tests := []struct {
		name         string
		opt          func(*sutConfig)
		wantResolved bool
	}{
		{
			name:         "default_config",
			opt:          func(*sutConfig) {},
			wantResolved: true,
		},
		{
			name: "explicitly_enable_mapping",
			opt: func(c *sutConfig) {
				c.vmConfig.ResolvePendingToLastExecuted = true
			},
			wantResolved: true,
		},
		{
			name: "explicitly_disable_mapping",
			opt: func(c *sutConfig) {
				c.vmConfig.ResolvePendingToLastExecuted = false
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := common.Address{'S', 't', 'e', 'p', 'h', 'e', 'n'}
			code := []byte{42}

			ctx, sut := newSUT(t,
				withAccount(addr, types.Account{
					Code: code,
				}),
				options.Func[sutConfig](tt.opt),
			)

			got, err := sut.EthClient().PendingCodeAt(ctx, addr)
			if !tt.wantResolved {
				require.ErrorContainsf(t, err, "pending", "%T.PendingCodeAt(%v)", sut.EthClient(), addr)
				return
			}
			require.NoErrorf(t, err, "%T.PendingCodeAt(%v)", sut.EthClient(), addr)
			assert.Equalf(t, code, got, "%T.PendingCodeAt(%v)", sut.EthClient(), addr)
		})
	}
}
