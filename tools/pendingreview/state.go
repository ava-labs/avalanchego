// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type ReviewState struct {
	Repo                 string             `json:"repo"`
	PRNumber             int                `json:"pr"`
	UserLogin            string             `json:"user"`
	ReviewID             string             `json:"review_id"`
	LastPublishedBody    string             `json:"last_published_body"`
	LastPublishedEntries []DraftReviewEntry `json:"last_published_entries,omitempty"`
	HTMLURL              string             `json:"html_url,omitempty"`
}

type StateStore struct {
	rootDir string
	log     logging.Logger
}

func NewStateStore(log logging.Logger, rootDir string) StateStore {
	return StateStore{
		rootDir: rootDir,
		log:     log,
	}
}

func defaultStateDir() string {
	if dir := os.Getenv("GH_PENDING_REVIEW_STATE_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("GH_DRAFT_REVIEW_STATE_DIR"); dir != "" {
		return dir
	}

	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome != "" {
		return filepath.Join(stateHome, "gh-pending-review")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".gh-pending-review/state"
	}
	return filepath.Join(homeDir, ".local", "state", "gh-pending-review")
}

func (s StateStore) Save(state ReviewState) error {
	path, err := s.pathFor(state.Repo, state.UserLogin, state.PRNumber)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	s.log.Info("saving review state",
		zap.String("repo", state.Repo),
		zap.Int("prNumber", state.PRNumber),
		zap.String("userLogin", state.UserLogin),
		zap.String("reviewID", state.ReviewID),
		zap.String("path", path),
	)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return stacktrace.Wrap(err)
	}

	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return stacktrace.Wrap(err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return stacktrace.Wrap(err)
	}
	s.log.Info("saved review state",
		zap.String("repo", state.Repo),
		zap.Int("prNumber", state.PRNumber),
		zap.String("userLogin", state.UserLogin),
		zap.String("reviewID", state.ReviewID),
		zap.String("path", path),
	)
	return nil
}

func (s StateStore) Load(repo string, userLogin string, prNumber int) (ReviewState, error) {
	path, err := s.pathFor(repo, userLogin, prNumber)
	if err != nil {
		return ReviewState{}, stacktrace.Wrap(err)
	}
	s.log.Debug("loading review state",
		zap.String("repo", repo),
		zap.Int("prNumber", prNumber),
		zap.String("userLogin", userLogin),
		zap.String("path", path),
	)

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ReviewState{}, stacktrace.Errorf("no stored review state for %s#%d as %s; run create first or use --force", repo, prNumber, userLogin)
		}
		return ReviewState{}, stacktrace.Wrap(err)
	}

	state, err := unmarshalReviewState(content)
	if err != nil {
		return ReviewState{}, stacktrace.Wrap(err)
	}
	s.log.Debug("loaded review state",
		zap.String("repo", repo),
		zap.Int("prNumber", prNumber),
		zap.String("userLogin", userLogin),
		zap.String("reviewID", state.ReviewID),
		zap.String("path", path),
	)
	return state, nil
}

func (s StateStore) Delete(repo string, userLogin string, prNumber int) error {
	path, err := s.pathFor(repo, userLogin, prNumber)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	s.log.Info("deleting review state",
		zap.String("repo", repo),
		zap.Int("prNumber", prNumber),
		zap.String("userLogin", userLogin),
		zap.String("path", path),
	)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return stacktrace.Wrap(err)
	}
	s.log.Info("deleted review state",
		zap.String("repo", repo),
		zap.Int("prNumber", prNumber),
		zap.String("userLogin", userLogin),
		zap.String("path", path),
	)
	return nil
}

func (s StateStore) LoadIfExists(repo string, userLogin string, prNumber int) (ReviewState, bool, error) {
	path, err := s.pathFor(repo, userLogin, prNumber)
	if err != nil {
		return ReviewState{}, false, stacktrace.Wrap(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ReviewState{}, false, nil
		}
		return ReviewState{}, false, stacktrace.Wrap(err)
	}

	state, err := unmarshalReviewState(content)
	if err != nil {
		return ReviewState{}, false, stacktrace.Wrap(err)
	}
	return state, true, nil
}

func unmarshalReviewState(content []byte) (ReviewState, error) {
	type reviewStateAlias struct {
		Repo                  string             `json:"repo"`
		PRNumber              int                `json:"pr"`
		UserLogin             string             `json:"user"`
		ReviewID              json.RawMessage    `json:"review_id"`
		LastPublishedBody     string             `json:"last_published_body"`
		LastPublishedComments []DraftReviewEntry `json:"last_published_comments,omitempty"`
		LastPublishedEntries  []DraftReviewEntry `json:"last_published_entries,omitempty"`
		HTMLURL               string             `json:"html_url,omitempty"`
	}

	var decoded reviewStateAlias
	if err := json.Unmarshal(content, &decoded); err != nil {
		return ReviewState{}, stacktrace.Wrap(err)
	}

	reviewID, err := decodeReviewID(decoded.ReviewID)
	if err != nil {
		return ReviewState{}, stacktrace.Wrap(err)
	}

	entries := decoded.LastPublishedEntries
	if len(entries) == 0 && len(decoded.LastPublishedComments) > 0 {
		entries = decoded.LastPublishedComments
	}

	return ReviewState{
		Repo:                 decoded.Repo,
		PRNumber:             decoded.PRNumber,
		UserLogin:            decoded.UserLogin,
		ReviewID:             reviewID,
		LastPublishedBody:    decoded.LastPublishedBody,
		LastPublishedEntries: normalizeDraftReviewEntries(entries),
		HTMLURL:              decoded.HTMLURL,
	}, nil
}

func decodeReviewID(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}

	var stringID string
	if err := json.Unmarshal(raw, &stringID); err == nil {
		return stringID, nil
	}

	var numericID int64
	if err := json.Unmarshal(raw, &numericID); err == nil {
		return strconv.FormatInt(numericID, 10), nil
	}
	return "", stacktrace.Errorf("decode review_id: unsupported JSON value %s", string(raw))
}

func (s StateStore) pathFor(repo string, userLogin string, prNumber int) (string, error) {
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return "", stacktrace.Wrap(err)
	}
	return filepath.Join(s.rootDir, owner, repoName, userLogin, fmt.Sprintf("%d.json", prNumber)), nil
}

func splitRepo(repo string) (string, string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", stacktrace.Errorf("invalid repo %q", repo)
	}
	return parts[0], parts[1], nil
}
