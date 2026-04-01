package draftreview

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ReviewState struct {
	Repo              string `json:"repo"`
	PRNumber          int    `json:"pr"`
	UserLogin         string `json:"user"`
	ReviewID          int64  `json:"review_id"`
	LastPublishedBody string `json:"last_published_body"`
	HTMLURL           string `json:"html_url,omitempty"`
}

type StateStore struct {
	rootDir string
}

func NewStateStore(rootDir string) StateStore {
	return StateStore{rootDir: rootDir}
}

func defaultStateDir() string {
	if dir := os.Getenv("GH_DRAFT_REVIEW_STATE_DIR"); dir != "" {
		return dir
	}

	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome != "" {
		return filepath.Join(stateHome, "gh-draft-review")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".gh-draft-review/state"
	}
	return filepath.Join(homeDir, ".local", "state", "gh-draft-review")
}

func (s StateStore) Save(state ReviewState) error {
	path, err := s.pathFor(state.Repo, state.UserLogin, state.PRNumber)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o644)
}

func (s StateStore) Load(repo string, userLogin string, prNumber int) (ReviewState, error) {
	path, err := s.pathFor(repo, userLogin, prNumber)
	if err != nil {
		return ReviewState{}, err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ReviewState{}, fmt.Errorf("no stored review state for %s#%d as %s; run create first or use --force", repo, prNumber, userLogin)
		}
		return ReviewState{}, err
	}

	var state ReviewState
	if err := json.Unmarshal(content, &state); err != nil {
		return ReviewState{}, err
	}
	return state, nil
}

func (s StateStore) Delete(repo string, userLogin string, prNumber int) error {
	path, err := s.pathFor(repo, userLogin, prNumber)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s StateStore) pathFor(repo string, userLogin string, prNumber int) (string, error) {
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.rootDir, owner, repoName, userLogin, fmt.Sprintf("%d.json", prNumber)), nil
}

func splitRepo(repo string) (string, string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo %q", repo)
	}
	return parts[0], parts[1], nil
}
