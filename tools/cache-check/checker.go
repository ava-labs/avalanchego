// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
)

const (
	taskCacheRestoreCheck   = "task-cache-restore"
	goModCacheRestoreCheck  = "go-mod-cache-restore"
	goUnitCacheRestoreCheck = "go-unit-cache-restore"
	goUnitTestResultsCheck  = "go-unit-test-results"
)

var (
	errUnknownCheck          = errors.New("unknown cache check")
	errCacheRestoreMiss      = errors.New("cache was not restored exactly")
	errGoModuleDownload      = errors.New("go downloaded a module")
	errGoTestResultMissing   = errors.New("go test result is missing")
	errGoTestResultNotCached = errors.New("go test result was not cached")
)

var knownChecks = []string{
	taskCacheRestoreCheck,
	goModCacheRestoreCheck,
	goUnitCacheRestoreCheck,
	goUnitTestResultsCheck,
}

type checkOptions struct {
	allowUncachedGoTestPackages []string
}

func check(log []byte, names []string) error {
	return checkWithOptions(log, names, checkOptions{})
}

func checkWithOptions(log []byte, names []string, options checkOptions) error {
	errs := make([]error, 0, len(names))
	for _, name := range names {
		if !slices.Contains(knownChecks, name) {
			errs = append(errs, fmt.Errorf("%w: %q", errUnknownCheck, name))
			continue
		}

		var err error
		switch name {
		case taskCacheRestoreCheck:
			err = checkCacheRestore(log, "task-cache-hit")
		case goModCacheRestoreCheck:
			err = errors.Join(
				checkCacheRestore(log, "go-mod-cache-hit"),
				checkNoGoModuleDownload(log),
			)
		case goUnitCacheRestoreCheck:
			err = checkCacheRestore(log, "go-unit-cache-hit")
		case goUnitTestResultsCheck:
			err = checkGoTestResults(log, options.allowUncachedGoTestPackages)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

func checkCacheRestore(log []byte, name string) error {
	expected := []byte(name + "=true")
	for _, line := range bytes.Split(log, []byte{'\n'}) {
		if bytes.Equal(bytes.TrimSpace(line), expected) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", errCacheRestoreMiss, expected)
}

func checkNoGoModuleDownload(log []byte) error {
	if bytes.Contains(log, []byte("go: downloading ")) {
		return errGoModuleDownload
	}
	return nil
}

func checkGoTestResults(log []byte, allowUncachedPackages []string) error {
	found := false
	for _, line := range bytes.Split(log, []byte{'\n'}) {
		fields := bytes.Fields(line)
		if len(fields) == 0 || !bytes.Equal(fields[0], []byte("ok")) {
			continue
		}

		found = true
		if slices.ContainsFunc(fields, func(field []byte) bool {
			return bytes.Equal(field, []byte("(cached)"))
		}) {
			continue
		}
		if len(fields) > 1 && slices.Contains(allowUncachedPackages, string(fields[1])) {
			continue
		}
		return fmt.Errorf("%w: %s", errGoTestResultNotCached, line)
	}
	if !found {
		return errGoTestResultMissing
	}
	return nil
}
