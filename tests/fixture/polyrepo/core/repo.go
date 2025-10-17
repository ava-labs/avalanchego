// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/google/go-github/v68/github"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// ParseRepoAndVersion parses a string like "repo@version" and returns the repo name and version
func ParseRepoAndVersion(input string) (string, string, error) {
	if input == "" {
		return "", "", stacktrace.Errorf("empty input")
	}

	parts := strings.Split(input, "@")
	if len(parts) > 2 {
		return "", "", stacktrace.Errorf("invalid format: multiple @ symbols")
	}

	repo := parts[0]
	version := ""
	if len(parts) == 2 {
		version = parts[1]
	}

	return repo, version, nil
}

// GetRepoClonePath returns the path where a repo should be cloned
func GetRepoClonePath(repoName, baseDir string) string {
	return filepath.Join(baseDir, repoName)
}

// CloneRepo clones a repository to the specified path
// depth: 0 for full clone, 1 for shallow clone (default), >1 for partial clone
// ref can be a branch name, tag, or commit SHA. For SHAs, the repository is cloned
// without SingleBranch and the ref is checked out after cloning.
func CloneRepo(log logging.Logger, url, path, ref string, depth int) error {
	log.Debug("entering CloneRepo",
		zap.String("url", url),
		zap.String("path", path),
		zap.String("ref", ref),
		zap.Int("depth", depth),
	)

	opts := &git.CloneOptions{
		URL: url,
	}

	// depth of 0 means full clone, otherwise set depth
	if depth > 0 {
		opts.Depth = depth
		log.Debug("setting clone depth",
			zap.Int("depth", depth),
		)
	} else {
		log.Debug("performing full clone (depth=0)")
	}

	// If ref is specified and looks like a branch or tag (not a SHA),
	// set it as the reference to clone.
	// For SHAs, we clone default branch then fetch the SHA separately
	isSHA := looksLikeSHA(ref)
	log.Debug("checked if ref is SHA",
		zap.String("ref", ref),
		zap.Bool("isSHA", isSHA),
	)

	if ref != "" && !isSHA {
		opts.ReferenceName = plumbing.NewBranchReferenceName(ref)
		opts.SingleBranch = true
		log.Debug("configuring git clone for branch/tag",
			zap.String("referenceName", ref),
			zap.Bool("singleBranch", true),
		)
	} else {
		// For SHA or no ref, clone default branch only
		opts.SingleBranch = true
		log.Debug("configuring git clone for default branch (single-branch)")
	}

	log.Debug("executing git clone")
	_, err := git.PlainClone(path, false, opts)
	if err != nil {
		return stacktrace.Errorf("failed to clone repository: %w", err)
	}

	log.Debug("git clone completed successfully",
		zap.String("path", path),
	)

	return nil
}

// looksLikeSHA returns true if the ref looks like a git commit SHA
func looksLikeSHA(ref string) bool {
	// Git SHAs are 40 character hex strings (or 7+ for short SHAs)
	// Check if it's at least 7 chars and all hex
	if len(ref) < 7 {
		return false
	}
	for _, c := range ref {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// CheckoutRef checks out a specific ref (tag, branch, or commit) in a repository
func CheckoutRef(log logging.Logger, repoPath, ref string) error {
	log.Debug("entering CheckoutRef",
		zap.String("repoPath", repoPath),
		zap.String("ref", ref),
	)

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return stacktrace.Errorf("failed to open repository: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return stacktrace.Errorf("failed to get worktree: %w", err)
	}

	// Try to resolve the ref
	log.Debug("resolving revision",
		zap.String("ref", ref),
	)
	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return stacktrace.Errorf("failed to resolve ref %s: %w", ref, err)
	}

	log.Debug("resolved revision to hash",
		zap.String("ref", ref),
		zap.String("hash", hash.String()),
	)

	log.Debug("executing git checkout",
		zap.String("hash", hash.String()),
	)
	err = w.Checkout(&git.CheckoutOptions{
		Hash: *hash,
	})
	if err != nil {
		return stacktrace.Errorf("failed to checkout ref %s: %w", ref, err)
	}

	log.Debug("checkout completed successfully")

	return nil
}

// getGitDir returns the git directory path and whether this is a worktree.
// For normal repos, returns (repoPath/.git, false, nil)
// For worktrees, returns (path-from-gitdir-file, true, nil)
func getGitDir(repoPath string) (string, bool, error) {
	gitPath := filepath.Join(repoPath, ".git")

	// Check if .git exists and is a file or directory
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", false, stacktrace.Errorf("failed to stat .git: %w", err)
	}

	// If .git is a directory, this is a normal repo
	if info.IsDir() {
		return gitPath, false, nil
	}

	// If .git is a file, this is a worktree - read the gitdir reference
	content, err := os.ReadFile(gitPath)
	if err != nil {
		return "", false, stacktrace.Errorf("failed to read .git file: %w", err)
	}

	// Parse "gitdir: /path/to/git/dir"
	contentStr := strings.TrimSpace(string(content))
	if !strings.HasPrefix(contentStr, "gitdir: ") {
		return "", false, stacktrace.Errorf("invalid .git file format: %s", contentStr)
	}

	gitDir := strings.TrimPrefix(contentStr, "gitdir: ")
	gitDir = strings.TrimSpace(gitDir)

	// If gitDir is relative, resolve it relative to repoPath
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoPath, gitDir)
	}

	return gitDir, true, nil
}

// GetCurrentRef returns the current ref (branch or commit) of a repository
func GetCurrentRef(log logging.Logger, repoPath string) (string, error) {
	log.Debug("getting current ref",
		zap.String("repoPath", repoPath),
	)

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", stacktrace.Errorf("failed to open repository: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return "", stacktrace.Errorf("failed to get HEAD: %w", err)
	}

	// If HEAD is a branch, return the branch name
	if head.Name().IsBranch() {
		branchName := head.Name().Short()
		log.Debug("HEAD is branch reference",
			zap.Bool("isBranch", true),
			zap.String("branchName", branchName),
		)
		return branchName, nil
	}

	// Otherwise return the commit hash (detached HEAD)
	commitHash := head.Hash().String()
	log.Debug("HEAD is detached (not a branch)",
		zap.String("commitHash", commitHash),
	)
	return commitHash, nil
}

// GetCommitSHA returns the 8-digit short SHA of the current commit
func GetCommitSHA(log logging.Logger, repoPath string) (string, error) {
	log.Debug("getting commit SHA",
		zap.String("repoPath", repoPath),
	)

	// Check if this is a worktree and get the actual git dir
	gitDir, isWorktree, err := getGitDir(repoPath)
	if err != nil {
		return "", stacktrace.Errorf("failed to get git directory: %w", err)
	}

	if isWorktree {
		log.Debug("detected worktree, using git command",
			zap.String("gitDir", gitDir),
		)
		// For worktrees, go-git can't read refs properly even with the git dir
		// Use git command directly
		cmd := exec.Command("git", "rev-parse", "--short=8", "HEAD")
		cmd.Dir = repoPath
		output, cmdErr := cmd.CombinedOutput()
		if cmdErr != nil {
			return "", stacktrace.Errorf("failed to get HEAD in worktree: %w", cmdErr)
		}
		shortSHA := strings.TrimSpace(string(output))
		log.Debug("got short commit SHA via git command",
			zap.String("shortSHA", shortSHA),
		)
		return shortSHA, nil
	}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", stacktrace.Errorf("failed to open repository: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return "", stacktrace.Errorf("failed to get HEAD: %w", err)
	}

	// Get the commit hash and return first 8 characters
	commitHash := head.Hash().String()
	if len(commitHash) < 8 {
		return commitHash, nil
	}

	shortSHA := commitHash[:8]
	log.Debug("got short commit SHA",
		zap.String("fullHash", commitHash),
		zap.String("shortSHA", shortSHA),
	)
	return shortSHA, nil
}

// GetTagsForCommit returns all tags that point to the given commit SHA
func GetTagsForCommit(log logging.Logger, repoPath string, commitSHA string) ([]string, error) {
	log.Debug("getting tags for commit",
		zap.String("repoPath", repoPath),
		zap.String("commitSHA", commitSHA),
	)

	// Check if this is a worktree
	_, isWorktree, err := getGitDir(repoPath)
	if err != nil {
		return nil, stacktrace.Errorf("failed to get git directory: %w", err)
	}

	if isWorktree {
		log.Debug("detected worktree, using git command")
		return getTagsViaGitCommand(log, repoPath, commitSHA)
	}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, stacktrace.Errorf("failed to open repository: %w", err)
	}

	// Get all tags
	tags, err := repo.Tags()
	if err != nil {
		return nil, stacktrace.Errorf("failed to get tags: %w", err)
	}

	var matchingTags []string
	err = tags.ForEach(func(ref *plumbing.Reference) error {
		// Get the object the tag points to
		tagHash := ref.Hash()

		// Check if it's an annotated tag (need to dereference)
		obj, err := repo.TagObject(tagHash)
		if err == nil {
			// This is an annotated tag, get the commit it points to
			tagHash = obj.Target
		}

		// Check if this tag points to our commit (compare full or short hash)
		hashStr := tagHash.String()
		if hashStr == commitSHA || (len(hashStr) >= 8 && hashStr[:8] == commitSHA) {
			tagName := ref.Name().Short()
			matchingTags = append(matchingTags, tagName)
			log.Debug("found matching tag",
				zap.String("tag", tagName),
				zap.String("hash", hashStr),
			)
		}

		return nil
	})
	if err != nil {
		return nil, stacktrace.Errorf("failed to iterate tags: %w", err)
	}

	log.Debug("finished getting tags for commit",
		zap.Int("matchingCount", len(matchingTags)),
		zap.Strings("tags", matchingTags),
	)

	return matchingTags, nil
}

// getTagsViaGitCommand is a fallback for worktrees where go-git doesn't work properly
func getTagsViaGitCommand(log logging.Logger, repoPath string, commitSHA string) ([]string, error) {
	cmd := exec.Command("git", "tag", "--points-at", commitSHA)
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If short SHA doesn't work, try with full SHA
		cmdFull := exec.Command("git", "rev-parse", commitSHA)
		cmdFull.Dir = repoPath
		fullOutput, fullErr := cmdFull.CombinedOutput()
		if fullErr == nil {
			fullSHA := strings.TrimSpace(string(fullOutput))
			cmd = exec.Command("git", "tag", "--points-at", fullSHA)
			cmd.Dir = repoPath
			output, err = cmd.CombinedOutput()
		}
		if err != nil {
			return nil, stacktrace.Errorf("failed to get tags via git command: %w", err)
		}
	}

	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" {
		return []string{}, nil
	}

	matchingTags := strings.Split(outputStr, "\n")
	log.Debug("got tags via git command",
		zap.Int("count", len(matchingTags)),
		zap.Strings("tags", matchingTags),
	)

	return matchingTags, nil
}

// GetBranchesForCommit returns all branches that point to the given commit SHA
func GetBranchesForCommit(log logging.Logger, repoPath string, commitSHA string) ([]string, error) {
	log.Debug("getting branches for commit",
		zap.String("repoPath", repoPath),
		zap.String("commitSHA", commitSHA),
	)

	// Check if this is a worktree
	_, isWorktree, err := getGitDir(repoPath)
	if err != nil {
		return nil, stacktrace.Errorf("failed to get git directory: %w", err)
	}

	if isWorktree {
		log.Debug("detected worktree, using git command")
		return getBranchesViaGitCommand(log, repoPath, commitSHA)
	}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, stacktrace.Errorf("failed to open repository: %w", err)
	}

	// Get all branches
	branches, err := repo.Branches()
	if err != nil {
		return nil, stacktrace.Errorf("failed to get branches: %w", err)
	}

	var matchingBranches []string
	err = branches.ForEach(func(ref *plumbing.Reference) error {
		// Get the commit hash this branch points to
		branchHash := ref.Hash().String()
		branchName := ref.Name().Short()

		// Check if this branch points to our commit (compare full or short hash)
		if branchHash == commitSHA || (len(branchHash) >= 8 && branchHash[:8] == commitSHA) {
			matchingBranches = append(matchingBranches, branchName)
			log.Debug("found matching branch",
				zap.String("branch", branchName),
				zap.String("hash", branchHash),
			)
		}

		return nil
	})
	if err != nil {
		return nil, stacktrace.Errorf("failed to iterate branches: %w", err)
	}

	log.Debug("finished getting branches for commit",
		zap.Int("matchingCount", len(matchingBranches)),
		zap.Strings("branches", matchingBranches),
	)

	return matchingBranches, nil
}

// getBranchesViaGitCommand is a fallback for worktrees where go-git doesn't work properly
func getBranchesViaGitCommand(log logging.Logger, repoPath string, commitSHA string) ([]string, error) {
	cmd := exec.Command("git", "branch", "--contains", commitSHA, "--format=%(refname:short)")
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If short SHA doesn't work, try with full SHA
		cmdFull := exec.Command("git", "rev-parse", commitSHA)
		cmdFull.Dir = repoPath
		fullOutput, fullErr := cmdFull.CombinedOutput()
		if fullErr == nil {
			fullSHA := strings.TrimSpace(string(fullOutput))
			cmd = exec.Command("git", "branch", "--contains", fullSHA, "--format=%(refname:short)")
			cmd.Dir = repoPath
			output, err = cmd.CombinedOutput()
		}
		if err != nil {
			return nil, stacktrace.Errorf("failed to get branches via git command: %w", err)
		}
	}

	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" {
		return []string{}, nil
	}

	matchingBranches := strings.Split(outputStr, "\n")
	log.Debug("got branches via git command",
		zap.Int("count", len(matchingBranches)),
		zap.Strings("branches", matchingBranches),
	)

	return matchingBranches, nil
}

// IsRepoDirty checks if a repository has uncommitted changes
func IsRepoDirty(log logging.Logger, repoPath string) (bool, error) {
	log.Debug("checking if repository is dirty",
		zap.String("repoPath", repoPath),
	)

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return false, stacktrace.Errorf("failed to open repository: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return false, stacktrace.Errorf("failed to get worktree: %w", err)
	}

	status, err := w.Status()
	if err != nil {
		return false, stacktrace.Errorf("failed to get status: %w", err)
	}

	isDirty := !status.IsClean()
	log.Debug("repository dirty check complete",
		zap.Bool("isDirty", isDirty),
		zap.Int("modifiedFiles", len(status)),
	)

	return isDirty, nil
}

// CloneOrUpdateRepo clones a repository or updates an existing one
// Handles the case where a SHA is requested with shallow clone by falling back to full clone
func CloneOrUpdateRepo(log logging.Logger, url, path, ref string, depth int, force bool) error {
	log.Debug("entering CloneOrUpdateRepo",
		zap.String("url", url),
		zap.String("path", path),
		zap.String("ref", ref),
		zap.Int("depth", depth),
		zap.Bool("force", force),
	)

	// Check if repo already exists
	_, err := git.PlainOpen(path)
	repoExists := err == nil
	log.Debug("checked if repository exists",
		zap.String("path", path),
		zap.Bool("exists", repoExists),
	)

	if repoExists {
		log.Debug("repository exists, checking force flag",
			zap.Bool("force", force),
		)
		if !force {
			log.Debug("returning error: repo exists and force=false")
			return ErrRepoAlreadyExists(filepath.Base(path), path)
		}
		log.Debug("proceeding to update existing repository")
		// Update existing repo
		return UpdateRepo(log, url, path, ref, force)
	}

	log.Debug("repository does not exist, proceeding to clone")

	// Special case: if ref is a SHA with shallow clone, use direct fetch to avoid double-fetch
	// This saves bandwidth by only fetching the specific SHA instead of default branch + SHA
	if ref != "" && looksLikeSHA(ref) && depth > 0 {
		log.Debug("ref is SHA with shallow clone, using direct fetch",
			zap.String("ref", ref),
		)

		// Expand short SHA to full 40-char SHA using GitHub API
		// GitHub's uploadpack.allowReachableSHA1InWant requires full SHA
		fullSHA := ref
		if len(ref) < 40 {
			owner, repo := parseGitHubURL(url)
			if owner != "" && repo != "" {
				log.Debug("expanding short SHA to full SHA via GitHub API",
					zap.String("shortSHA", ref),
				)
				client := github.NewClient(nil)
				ctx := context.Background()
				commit, _, apiErr := client.Repositories.GetCommit(ctx, owner, repo, ref, nil)
				if apiErr != nil {
					return stacktrace.Errorf("failed to expand SHA %s via GitHub API: %w", ref, apiErr)
				}
				fullSHA = commit.GetSHA()
				log.Debug("expanded SHA",
					zap.String("shortSHA", ref),
					zap.String("fullSHA", fullSHA),
				)
			}
		}

		// Initialize empty repo
		// We use exec.Command for git fetch because go-git doesn't support uploadpack.allowReachableSHA1InWant
		// See: https://github.com/go-git/go-git/issues/323
		log.Debug("initializing empty repository")
		initCmd := exec.Command("git", "init")
		initCmd.Dir = path
		if err := os.MkdirAll(path, 0o755); err != nil {
			return stacktrace.Errorf("failed to create directory %s: %w", path, err)
		}
		if output, err := initCmd.CombinedOutput(); err != nil {
			return stacktrace.Errorf("failed to init repository: %w (output: %s)", err, string(output))
		}

		// Add remote
		log.Debug("adding remote origin",
			zap.String("url", url),
		)
		remoteCmd := exec.Command("git", "remote", "add", "origin", url)
		remoteCmd.Dir = path
		if output, err := remoteCmd.CombinedOutput(); err != nil {
			return stacktrace.Errorf("failed to add remote: %w (output: %s)", err, string(output))
		}

		// Fetch specific SHA with depth=1
		// GitHub supports uploadpack.allowReachableSHA1InWant for fetching arbitrary reachable commits
		// See: https://stackoverflow.com/questions/31278902/how-to-shallow-clone-a-specific-commit-with-depth-1
		log.Debug("fetching SHA with git command",
			zap.String("fullSHA", fullSHA),
			zap.Int("depth", depth),
		)
		fetchCmd := exec.Command("git", "fetch", "--depth", "1", "origin", fullSHA)
		fetchCmd.Dir = path
		output, fetchErr := fetchCmd.CombinedOutput()
		if fetchErr != nil {
			return stacktrace.Errorf("failed to fetch SHA %s: %w (output: %s)", fullSHA, fetchErr, string(output))
		}

		log.Debug("fetch SHA succeeded",
			zap.String("output", string(output)),
		)

		// Checkout the SHA
		return CheckoutRef(log, path, ref)
	}

	// Standard clone for branches/tags or full clones
	log.Debug("attempting clone with specified depth",
		zap.Int("depth", depth),
	)
	err = CloneRepo(log, url, path, ref, depth)
	if err != nil {
		return stacktrace.Errorf("failed to clone repository: %w", err)
	}

	log.Debug("clone completed successfully")
	return nil
}

// parseGitHubURL extracts owner and repo from a GitHub URL
// Returns empty strings if not a GitHub URL
func parseGitHubURL(url string) (owner, repo string) {
	// Match patterns like:
	// https://github.com/owner/repo
	// https://github.com/owner/repo.git
	// git@github.com:owner/repo.git
	patterns := []string{
		`github\.com[:/]([^/]+)/([^/\.]+)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(url)
		if len(matches) == 3 {
			return matches[1], matches[2]
		}
	}
	return "", ""
}

// findGitHubBranchForSHA queries GitHub API to find which branch has the SHA as HEAD
// Returns empty string if not found or if there's an error
func findGitHubBranchForSHA(log logging.Logger, url, sha string) string {
	owner, repo := parseGitHubURL(url)
	if owner == "" || repo == "" {
		log.Debug("not a GitHub URL, skipping branch lookup",
			zap.String("url", url),
		)
		return ""
	}

	log.Debug("querying GitHub API for branch containing SHA",
		zap.String("owner", owner),
		zap.String("repo", repo),
		zap.String("sha", sha),
	)

	client := github.NewClient(nil)
	ctx := context.Background()

	branches, _, err := client.Repositories.ListBranchesHeadCommit(ctx, owner, repo, sha)
	if err != nil {
		log.Debug("failed to query GitHub API",
			zap.Error(err),
		)
		return ""
	}

	if len(branches) == 0 {
		log.Debug("no branches found with SHA as HEAD")
		return ""
	}

	// Return the first branch found
	branchName := branches[0].GetName()
	log.Debug("found branch for SHA",
		zap.String("branch", branchName),
	)
	return branchName
}

// UpdateRepo updates an existing repository to a specific ref
// If force is true and trying to update a shallow clone to a SHA, it will
// proactively remove the repo and re-clone it with full depth to avoid issues
// with incomplete history (which can cause problems for tools like nix).
func UpdateRepo(log logging.Logger, url, repoPath, ref string, force bool) error {
	log.Debug("entering UpdateRepo",
		zap.String("url", url),
		zap.String("repoPath", repoPath),
		zap.String("ref", ref),
		zap.Bool("force", force),
	)

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return stacktrace.Errorf("failed to open repository: %w", err)
	}

	// Check if this is a shallow clone
	isShallow := isShallowRepo(repoPath)
	log.Debug("checked if repository is shallow",
		zap.Bool("isShallow", isShallow),
	)

	// If we're updating a shallow clone to a SHA with --force, remove and re-clone
	// to avoid inconsistent shallow boundaries. The issue: shallow cloning main then
	// checking out a different SHA leaves the shallow file pointing to main's boundaries,
	// causing "object not found" errors when tools like nix try to access parent commits.
	if force && isShallow && looksLikeSHA(ref) {
		log.Info("shallow clone with SHA detected, removing and re-cloning at target SHA",
			zap.String("ref", ref),
			zap.String("path", repoPath),
		)
		os.RemoveAll(repoPath)

		// Try to find which branch has this SHA as HEAD (GitHub only)
		// This allows us to do a single-branch shallow clone (much smaller)
		branch := findGitHubBranchForSHA(log, url, ref)
		if branch != "" {
			log.Info("found branch for SHA, cloning with single-branch",
				zap.String("branch", branch),
			)
			// Clone the specific branch shallow, then checkout the SHA
			err = CloneRepo(log, url, repoPath, branch, 1)
			if err == nil {
				return CheckoutRef(log, repoPath, ref)
			}
			log.Info("single-branch clone failed, falling back to multi-branch",
				zap.Error(err),
			)
		}

		// Fallback: clone without single-branch (fetches all branches, larger but more reliable)
		err = CloneRepo(log, url, repoPath, ref, 1)
		if err != nil {
			// If shallow clone fails, fallback to full clone
			log.Info("shallow clone failed, retrying with full depth",
				zap.Error(err),
			)
			err = CloneRepo(log, url, repoPath, ref, 0)
			if err != nil {
				return err
			}
		}
		return CheckoutRef(log, repoPath, ref)
	}

	// Fetch latest changes
	log.Debug("fetching latest changes from remote")
	err = repo.Fetch(&git.FetchOptions{
		RefSpecs: []config.RefSpec{"refs/*:refs/*"},
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return stacktrace.Errorf("failed to fetch: %w", err)
	}

	if err == git.NoErrAlreadyUpToDate {
		log.Debug("repository already up to date")
	} else {
		log.Debug("fetch completed successfully")
	}

	// Checkout the ref
	log.Debug("checking out ref after fetch",
		zap.String("ref", ref),
	)
	return CheckoutRef(log, repoPath, ref)
}

// isShallowRepo checks if a repository is a shallow clone
func isShallowRepo(repoPath string) bool {
	shallowFile := filepath.Join(repoPath, ".git", "shallow")
	_, err := os.Stat(shallowFile)
	return err == nil
}
