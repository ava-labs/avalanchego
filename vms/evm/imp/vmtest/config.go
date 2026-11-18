// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmtest

import (
	"context"
	"testing"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/vms/evm/evmclient"
	"github.com/ava-labs/avalanchego/vms/evm/imp/vmconfig"
)

// TestConfigResolvePendingToLastExecuted tests that the VM created by newSUT
// honours [vmconfig.Config.ResolvePendingToLastExecuted].
func TestConfigResolvePendingToLastExecuted[C any, S interface{ EthClient() *evmclient.Client }](
	t *testing.T,
	newSUT func(testing.TB, ...SUTOption[C]) (context.Context, S),
) {
	// This test only exercises plumbing of the config. A full test of
	// functionality is performed in the SAE code.

	tests := []struct {
		name    string
		opt     func(*vmconfig.Config)
		wantErr testerr.Want
	}{
		{
			name: "default_config",
			opt:  func(*vmconfig.Config) {},
		},
		{
			name: "explicitly_enable_mapping",
			opt: func(c *vmconfig.Config) {
				c.ResolvePendingToLastExecuted = true
			},
		},
		{
			name: "explicitly_disable_mapping",
			opt: func(c *vmconfig.Config) {
				c.ResolvePendingToLastExecuted = false
			},
			wantErr: testerr.Contains("pending"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := common.Address{'S', 't', 'e', 'p', 'h', 'e', 'n'}
			code := []byte{42}

			ctx, sut := newSUT(t,
				WithAccount[C](addr, types.Account{
					Code: code,
				}),
				WithVMConfig[C](tt.opt),
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
