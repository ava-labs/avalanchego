// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"strconv"
	"strings"
)

func Parse(s string) (*Semantic, error) {
	if !strings.HasPrefix(s, "v") {
		return nil, fmt.Errorf("version string %q missing required prefix", s)
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

func ParseApplication(s string) (*Application, error) {
	if !strings.HasPrefix(s, "avalanche/") {
		return nil, fmt.Errorf("application string %q missing required prefix", s)
	}

	s = s[10:]
	major, minor, patch, err := parseVersions(s)
	if err != nil {
		return nil, err
	}

	return &Application{
		Major: major,
		Minor: minor,
		Patch: patch,
	}, nil
}

func parseVersions(s string) (int, int, int, error) {
	splitVersion := strings.SplitN(s, ".", 3)
	if len(splitVersion) != 3 {
		return 0, 0, 0, fmt.Errorf("failed to parse %s as a version", s)
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
