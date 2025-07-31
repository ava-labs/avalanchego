// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thepudds/fzgen/fuzzer"
)

func FuzzExpiryEntryMarshal(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)

		var entry ExpiryEntry
		fz := fuzzer.NewFuzzer(data)
		fz.Fill(&entry)

		marshalledData := entry.Marshal()

		var parsedEntry ExpiryEntry
		require.NoError(parsedEntry.Unmarshal(marshalledData))
		require.Equal(entry, parsedEntry)
	})
}

func FuzzExpiryEntryLessAndMarshalOrdering(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		var (
			entry0 ExpiryEntry
			entry1 ExpiryEntry
		)
		fz := fuzzer.NewFuzzer(data)
		fz.Fill(&entry0, &entry1)

		key0 := entry0.Marshal()
		key1 := entry1.Marshal()
		require.Equal(
			t,
			entry0.Less(entry1),
			bytes.Compare(key0, key1) == -1,
		)
	})
}

func FuzzExpiryEntryUnmarshal(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)

		var entry ExpiryEntry
		if err := entry.Unmarshal(data); err != nil {
			require.ErrorIs(err, errUnexpectedExpiryEntryLength)
			return
		}

		marshalledData := entry.Marshal()
		require.Equal(data, marshalledData)
	})
}
