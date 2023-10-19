// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ava-labs/avalanchego/utils/math"
)

var (
	errMissingDelimiter     = errors.New("missing delimiter")
	errMissingVersionPrefix = errors.New("missing required version prefix")
	errMissingVersions      = errors.New("missing version numbers")
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

func ParseApplication(s string) (*Application, error) {
	delimiter := strings.Index(s, "/")
	if delimiter == -1 {
		return nil, fmt.Errorf("failed to parse %s: %w", s, errMissingDelimiter)
	}

	client := s[:delimiter]
	versionIndex := math.Min(delimiter+1, len(s)-1)
	major, minor, patch, err := parseVersions(s[versionIndex:])
	if err != nil {
		return nil, err
	}

	return &Application{
		Name:  client,
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
