// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestStateStoreSaveLoadDelete(t *testing.T) {
	t.Parallel()

	store := NewStateStore(logging.NoLog{}, t.TempDir())
	state := ReviewState{
		Repo:              "ava-labs/avalanchego",
		PRNumber:          5168,
		UserLogin:         "maru-ava",
		ReviewID:          123,
		LastPublishedBody: "test",
		HTMLURL:           "https://example.invalid/review/123",
	}

	require.NoError(t, store.Save(state))

	path := filepath.Join(store.rootDir, "ava-labs", "avalanchego", "maru-ava", "5168.json")
	_, err := os.Stat(path)
	require.NoError(t, err)

	loaded, err := store.Load(state.Repo, state.UserLogin, state.PRNumber)
	require.NoError(t, err)
	require.True(t, reflect.DeepEqual(loaded, state), "unexpected loaded state: got %+v want %+v", loaded, state)

	require.NoError(t, store.Delete(state.Repo, state.UserLogin, state.PRNumber))
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}
