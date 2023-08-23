// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/ava-labs/avalanchego/ids"
)

func TestThrottledHandlerAppRequest(t *testing.T) {
	tests := []struct {
		name      string
		Throttler Throttler
		wantErr   bool
	}{
		{
			name:      "throttled",
			Throttler: NewTokenBucketThrottler(rate.Every(time.Second), 1),
		},
		{
			name:      "throttler errors",
			Throttler: NewTokenBucketThrottler(rate.Every(time.Second), 0),
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			handler := ThrottledHandler{
				Handler:   NoOpHandler{},
				Throttler: tt.Throttler,
			}
			_, err := handler.AppRequest(context.Background(), ids.GenerateTestNodeID(), time.Time{}, []byte("foobar"))
			if tt.wantErr {
				require.NotNil(err)
			} else {
				require.Nil(err)
			}
		})
	}
}
