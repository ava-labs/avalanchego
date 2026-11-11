// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"testing"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/assert"
)

func TestConfigResolvePendingToLastExecuted(t *testing.T) {
	// This test only exercises plumbing of the config. A full test of
	// functionality is performed in the SAE code.

	tests := []struct {
		name    string
		opt     func(*sutConfig)
		wantErr testerr.Want
	}{
		{
			name: "default_config",
			opt:  func(*sutConfig) {},
		},
		{
			name: "explicitly_enable_mapping",
			opt: func(c *sutConfig) {
				c.vmConfig.ResolvePendingToLastExecuted = true
			},
		},
		{
			name: "explicitly_disable_mapping",
			opt: func(c *sutConfig) {
				c.vmConfig.ResolvePendingToLastExecuted = false
			},
			wantErr: testerr.Contains("pending"),
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
			if diff := testerr.Diff(err, tt.wantErr); diff != "" {
				t.Fatalf("%T.PendingCodeAt(%v): %s", sut.EthClient(), addr, diff)
			}
			if tt.wantErr != nil {
				return
			}
			assert.Equalf(t, code, got, "%T.PendingCodeAt(%v)", sut.EthClient(), addr)
		})
	}
}
