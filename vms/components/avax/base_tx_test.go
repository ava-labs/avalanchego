// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifyMemoFieldLength(t *testing.T) {
	tests := []struct {
		name            string
		memo            []byte
		isDurangoActive bool
		want            error
	}{
		{
			name:            "empty_memo_pre_durango",
			memo:            nil,
			isDurangoActive: false,
			want:            nil,
		},
		{
			name:            "non_empty_memo_pre_durango",
			memo:            []byte("memo"),
			isDurangoActive: false,
			want:            nil,
		},
		{
			name:            "empty_memo_post_durango",
			memo:            nil,
			isDurangoActive: true,
			want:            nil,
		},
		{
			name:            "non_empty_memo_post_durango",
			memo:            []byte("memo"),
			isDurangoActive: true,
			want:            ErrMemoTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifyMemoFieldLength(tt.memo, tt.isDurangoActive)
			require.ErrorIs(t, got, tt.want)
		})
	}
}
