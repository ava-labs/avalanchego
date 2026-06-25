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
		want    config
		wantErr error
	}{
		{
			name: "empty_input",
			in:   nil,
			want: config{},
		},
		{
			name: "empty_json_object",
			in:   []byte(`{}`),
			want: config{},
		},
		{
			name: "price_target",
			in:   []byte(`{"min-price-target":1000}`),
			want: config{PriceTarget: utils.PointerTo(gas.Price(1000))},
		},
		{
			name: "explicit_zero",
			in:   []byte(`{"min-price-target":0}`),
			want: config{PriceTarget: utils.PointerTo(gas.Price(0))},
		},
		{
			name:    "invalid_json",
			in:      []byte(`{not json`),
			wantErr: errInvalidConfig,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseConfig(tt.in)
			require.ErrorIsf(t, err, tt.wantErr, "parseConfig(%q)", tt.in)
			if tt.wantErr != nil {
				return
			}
			require.Equalf(t, tt.want, got, "parseConfig(%q)", tt.in)
		})
	}
}
