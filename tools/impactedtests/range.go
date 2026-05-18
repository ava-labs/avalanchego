// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"errors"
	"strings"
)

type diffRange struct {
	baseRev            string
	headRev            string
	includeWorkingTree bool
}

func parseDiffRange(input string) (diffRange, error) {
	if strings.Contains(input, "...") || strings.Count(input, "..") != 1 {
		return diffRange{}, errors.New("must contain exactly one '..' separator")
	}

	parts := strings.SplitN(input, "..", 2)
	baseRev := strings.TrimSpace(parts[0])
	headRev := strings.TrimSpace(parts[1])
	if baseRev == "" {
		return diffRange{}, errors.New("base revision must not be empty")
	}

	return diffRange{
		baseRev:            baseRev,
		headRev:            headRev,
		includeWorkingTree: headRev == "",
	}, nil
}
