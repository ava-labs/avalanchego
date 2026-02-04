// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

type emptyDB struct {
	DB[struct{}, struct{}]
}

type emptyMarshaler struct {
	Marshaler[struct{}]
}

func Test_SyncerInitialization(t *testing.T) {
	db := &emptyDB{}
	m := &emptyMarshaler{}
	c := p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, p2p.NoOpHandler{})

	tests := []struct {
		name        string
		init        func() (*Syncer[struct{}, struct{}], error)
		expectedErr error
	}{
		{
			name: "all good",
			init: func() (*Syncer[struct{}, struct{}], error) {
				return NewSyncer[struct{}, struct{}](db, ids.Empty, Config{}, c, c, m, m)
			},
			expectedErr: nil,
		},
		{
			name: "no database",
			init: func() (*Syncer[struct{}, struct{}], error) {
				return NewSyncer[struct{}, struct{}](nil, ids.Empty, Config{}, c, c, m, m)
			},
			expectedErr: ErrNoDatabaseProvided,
		},
		{
			name: "no range proof marshaler",
			init: func() (*Syncer[struct{}, struct{}], error) {
				return NewSyncer[struct{}, struct{}](db, ids.Empty, Config{}, c, c, nil, m)
			},
			expectedErr: ErrNoRangeProofMarshalerProvided,
		},
		{
			name: "no change proof marshaler",
			init: func() (*Syncer[struct{}, struct{}], error) {
				return NewSyncer[struct{}, struct{}](db, ids.Empty, Config{}, c, c, m, nil)
			},
			expectedErr: ErrNoChangeProofMarshalerProvided,
		},
		{
			name: "no range proof client",
			init: func() (*Syncer[struct{}, struct{}], error) {
				return NewSyncer[struct{}, struct{}](db, ids.Empty, Config{}, nil, c, m, m)
			},
			expectedErr: ErrNoRangeProofClientProvided,
		},
		{
			name: "no change proof client",
			init: func() (*Syncer[struct{}, struct{}], error) {
				return NewSyncer[struct{}, struct{}](db, ids.Empty, Config{}, c, nil, m, m)
			},
			expectedErr: ErrNoChangeProofClientProvided,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncer, err := tt.init()
			if tt.expectedErr != nil {
				require.ErrorIs(t, err, tt.expectedErr)
				require.Nil(t, syncer)
			} else {
				require.NoError(t, err)
				require.NotNil(t, syncer)
			}
		})
	}
}

func Test_Midpoint(t *testing.T) {
	require := require.New(t)

	mid := midPoint(maybe.Some([]byte{1, 255}), maybe.Some([]byte{2, 1}))
	require.Equal(maybe.Some([]byte{2, 0}), mid)

	mid = midPoint(maybe.Nothing[[]byte](), maybe.Some([]byte{255, 255, 0}))
	require.Equal(maybe.Some([]byte{127, 255, 128}), mid)

	mid = midPoint(maybe.Some([]byte{255, 255, 255}), maybe.Some([]byte{255, 255}))
	require.Equal(maybe.Some([]byte{255, 255, 127, 128}), mid)

	mid = midPoint(maybe.Nothing[[]byte](), maybe.Some([]byte{255}))
	require.Equal(maybe.Some([]byte{127, 127}), mid)

	mid = midPoint(maybe.Some([]byte{1, 255}), maybe.Some([]byte{255, 1}))
	require.Equal(maybe.Some([]byte{128, 128}), mid)

	mid = midPoint(maybe.Some([]byte{140, 255}), maybe.Some([]byte{141, 0}))
	require.Equal(maybe.Some([]byte{140, 255, 127}), mid)

	mid = midPoint(maybe.Some([]byte{126, 255}), maybe.Some([]byte{127}))
	require.Equal(maybe.Some([]byte{126, 255, 127}), mid)

	mid = midPoint(maybe.Nothing[[]byte](), maybe.Nothing[[]byte]())
	require.Equal(maybe.Some([]byte{127}), mid)

	low := midPoint(maybe.Nothing[[]byte](), mid)
	require.Equal(maybe.Some([]byte{63, 127}), low)

	high := midPoint(mid, maybe.Nothing[[]byte]())
	require.Equal(maybe.Some([]byte{191}), high)

	mid = midPoint(maybe.Some([]byte{255, 255}), maybe.Nothing[[]byte]())
	require.Equal(maybe.Some([]byte{255, 255, 127, 127}), mid)

	mid = midPoint(maybe.Some([]byte{255}), maybe.Nothing[[]byte]())
	require.Equal(maybe.Some([]byte{255, 127, 127}), mid)

	for i := 0; i < 5000; i++ {
		r := rand.New(rand.NewSource(int64(i)))

		start := make([]byte, r.Intn(99)+1)
		_, err := r.Read(start)
		require.NoError(err)

		end := make([]byte, r.Intn(99)+1)
		_, err = r.Read(end)
		require.NoError(err)

		for bytes.Equal(start, end) {
			_, err = r.Read(end)
			require.NoError(err)
		}

		if bytes.Compare(start, end) == 1 {
			start, end = end, start
		}

		mid = midPoint(maybe.Some(start), maybe.Some(end))
		require.Equal(-1, bytes.Compare(start, mid.Value()))
		require.Equal(-1, bytes.Compare(mid.Value(), end))
	}
}
