package draftreview

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStateStoreSaveLoadDelete(t *testing.T) {
	t.Parallel()

	store := NewStateStore(t.TempDir())
	state := ReviewState{
		Repo:              "ava-labs/avalanchego",
		PRNumber:          5168,
		UserLogin:         "maru-ava",
		ReviewID:          123,
		LastPublishedBody: "test",
		HTMLURL:           "https://example.invalid/review/123",
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	path := filepath.Join(store.rootDir, "ava-labs", "avalanchego", "maru-ava", "5168.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected state file at %s: %v", path, err)
	}

	loaded, err := store.Load(state.Repo, state.UserLogin, state.PRNumber)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded != state {
		t.Fatalf("unexpected loaded state: got %+v want %+v", loaded, state)
	}

	if err := store.Delete(state.Repo, state.UserLogin, state.PRNumber); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected state file to be removed, got: %v", err)
	}
}
