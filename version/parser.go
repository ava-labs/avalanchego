// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	errMissingApplicationPrefix = errors.New("missing required application prefix")
	errMissingVersions          = errors.New("missing version numbers")
)

func ParseApplication(s string) (*Application, error) {
	if !strings.HasPrefix(s, "avalanche/") {
		return nil, fmt.Errorf("%w: %q", errMissingApplicationPrefix, s)
	}

	s = s[10:]
	splitVersion := strings.SplitN(s, ".", 3)
	if numSeperators := len(splitVersion); numSeperators != 3 {
		return nil, fmt.Errorf("%w: expected 3 only got %d", errMissingVersions, numSeperators)
	}

	major, err := strconv.Atoi(splitVersion[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s as a version: %w", s, err)
	}

	minor, err := strconv.Atoi(splitVersion[1])
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s as a version: %w", s, err)
	}

	patch, err := strconv.Atoi(splitVersion[2])
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s as a version: %w", s, err)
	}

	return &Application{
		Major: major,
		Minor: minor,
		Patch: patch,
	}, nil
}
