// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
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
	errUnknownCheck       = errors.New("unknown cache check")
	errCheckUnimplemented = errors.New("cache check is not implemented")
)

var knownChecks = []string{
	taskCacheRestoreCheck,
	goModCacheRestoreCheck,
	goUnitCacheRestoreCheck,
	goUnitTestResultsCheck,
}

func check(log []byte, names []string) error {
	errs := make([]error, 0, len(names))
	for _, name := range names {
		if !slices.Contains(knownChecks, name) {
			errs = append(errs, fmt.Errorf("%w: %q", errUnknownCheck, name))
			continue
		}
		if err := checkUnimplemented(name, log); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func checkUnimplemented(name string, _ []byte) error {
	return fmt.Errorf("%w: %q", errCheckUnimplemented, name)
}
