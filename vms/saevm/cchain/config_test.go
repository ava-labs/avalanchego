// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/gas"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		in      []byte
		want    Config
		wantErr bool
	}{
		{
			name: "empty_input",
			in:   nil,
			want: Config{},
		},
		{
			name: "empty_json_object",
			in:   []byte(`{}`),
			want: Config{},
		},
		{
			name: "price_target",
			in:   []byte(`{"min-price-target":1000}`),
			want: Config{PriceTarget: utils.PointerTo(gas.Price(1000))},
		},
		{
			name:    "invalid_json",
			in:      []byte(`{not json`),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseConfig(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
