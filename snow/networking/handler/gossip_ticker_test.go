// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A nil channel is never ready in a select, so the gossip arm of the
// dispatcher is effectively disabled if g.C == nil.
func TestGossipTicker(t *testing.T) {
	tests := []struct {
		name    string
		freq    time.Duration
		wantNil bool
	}{
		{
			name:    "disabled",
			freq:    0,
			wantNil: true,
		},
		{
			name:    "enabled",
			freq:    time.Millisecond,
			wantNil: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newGossipTicker(tt.freq)
			defer g.Stop()

			if tt.wantNil {
				require.Nil(t, g.C)
			} else {
				require.NotNil(t, g.C)
			}
		})
	}
}
