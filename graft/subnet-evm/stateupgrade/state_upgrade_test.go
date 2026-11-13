// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateupgrade

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEqual(t *testing.T) {
	tests := []struct {
		name    string
		upgrade string
		other   string // if empty, round-trips upgrade
		want    bool
	}{
		{
			name:    "round-trip_full_entry",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234","storage":{"0x0000000000000000000000000000000000000000000000000000000000000001":"0x0000000000000000000000000000000000000000000000000000000000000002"},"balanceChange":"100"}}}`,
			want:    true,
		},
		{
			name:    "round-trip_empty_storage_dropped_by_omitempty",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234","storage":{}}}}`,
			want:    true,
		},
		{
			name:    "round-trip_empty_code_dropped_by_omitempty",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x","balanceChange":"0x1"}}}`,
			want:    true,
		},
		{
			name:    "round-trip_decimal_zero_balance_change_re-marshaled_as_hex",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"0"}}}`,
			want:    true,
		},
		{
			name:    "hex_and_decimal_balance_change_compare_by_value",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"100"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"0x64"}}}`,
			want:    true,
		},
		{
			name:    "different_timestamp",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			other:   `{"blockTimestamp":2,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			want:    false,
		},
		{
			name:    "different_code",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x125678"}}}`,
			want:    false,
		},
		{
			name:    "different_storage_value",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"storage":{"0x0000000000000000000000000000000000000000000000000000000000000001":"0x0000000000000000000000000000000000000000000000000000000000000002"}}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"storage":{"0x0000000000000000000000000000000000000000000000000000000000000001":"0x0000000000000000000000000000000000000000000000000000000000000003"}}}}`,
			want:    false,
		},
		{
			name:    "different_balance_change",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"1"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"2"}}}`,
			want:    false,
		},
		{
			name:    "removed_balance_change",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234","balanceChange":"0"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			want:    false,
		},
		{
			name:    "extra_account",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"},"0x0200000000000000000000000000000000000000":{"code":"0xff"}}}`,
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a, b Block
			require.NoError(t, json.Unmarshal([]byte(tt.upgrade), &a), "json.Unmarshal(a)")

			other := []byte(tt.other)
			if len(other) == 0 {
				var err error
				other, err = json.Marshal(a)
				require.NoError(t, err, "json.Marshal(a) for round-trip")
			}
			require.NoError(t, json.Unmarshal(other, &b), "json.Unmarshal(b)")

			require.Equal(t, tt.want, a.Equal(&b), "a.Equal(&b)")
			require.Equal(t, tt.want, b.Equal(&a), "b.Equal(&a)")
		})
	}
}
