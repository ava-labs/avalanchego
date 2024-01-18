// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	errMissingVersionPrefix     = errors.New("missing required version prefix")
	errMissingApplicationPrefix = errors.New("missing required application prefix")
	errMissingVersions          = errors.New("missing version numbers")
)

func Parse(s string) (*Semantic, error) {
	if !strings.HasPrefix(s, "v") {
		return nil, fmt.Errorf("%w: %q", errMissingVersionPrefix, s)
	}

	s = s[1:]
	major, minor, patch, err := parseVersions(s)
	if err != nil {
		return nil, err
	}

	return &Semantic{
		Major: major,
		Minor: minor,
		Patch: patch,
	}, nil
}

// TODO: Remove after v1.11.x is activated
func ParseLegacyApplication(s string) (*Application, error) {
	prefix := fmt.Sprintf("%s/", LegacyAppName)
	if !strings.HasPrefix(s, prefix) {
		return nil, fmt.Errorf("%w: %q", errMissingApplicationPrefix, s)
	}

	s = s[len(prefix):]
	major, minor, patch, err := parseVersions(s)
	if err != nil {
		return nil, err
	}

	return &Application{
		Name:  Client, // Convert the legacy name to the current client name
		Major: major,
		Minor: minor,
		Patch: patch,
	}, nil
}

func parseVersions(s string) (int, int, int, error) {
	splitVersion := strings.SplitN(s, ".", 3)
	if numSeperators := len(splitVersion); numSeperators != 3 {
		return 0, 0, 0, fmt.Errorf("%w: expected 3 only got %d", errMissingVersions, numSeperators)
	}

	major, err := strconv.Atoi(splitVersion[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse %s as a version: %w", s, err)
	}

	minor, err := strconv.Atoi(splitVersion[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse %s as a version: %w", s, err)
	}

	patch, err := strconv.Atoi(splitVersion[2])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse %s as a version: %w", s, err)
	}

	return major, minor, patch, nil
}
