// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetAssetDescriptionC4T(t *testing.T) {
	_, vm, s, _, genesisTx := setup(t, true)
	defer func() {
		if err := vm.Shutdown(context.TODO()); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	avaxAssetID := genesisTx.ID()

	type args struct {
		in0   *http.Request
		args  *GetAssetDescriptionArgs
		reply *GetAssetDescriptionReply
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		want    []string
	}{
		{
			name: "With given assetId",
			args: args{
				in0:   nil,
				reply: &GetAssetDescriptionReply{},
				args: &GetAssetDescriptionArgs{
					AssetID: avaxAssetID.String(),
				},
			},
			want: []string{"AVAX", "SYMB", avaxAssetID.String()},
		},
		{
			name: "Without assetId",
			args: args{
				in0:   nil,
				reply: &GetAssetDescriptionReply{},
				args: &GetAssetDescriptionArgs{
					AssetID: avaxAssetID.String(),
				},
			},
			want: []string{"AVAX", "SYMB", vm.feeAssetID.String()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.GetAssetDescription(tt.args.in0, tt.args.args, tt.args.reply); (err != nil) != tt.wantErr {
				t.Fatal(err)
			}

			require.Equal(t, tt.want[0], tt.args.reply.Name, "Wrong name returned from GetAssetDescription %s", tt.args.reply.Name)
			require.Equal(t, tt.want[1], tt.args.reply.Symbol, "Wrong symbol returned from GetAssetDescription %s", tt.args.reply.Symbol)
			require.Equal(t, tt.want[2], tt.args.reply.AssetID.String())
		})
	}
}
