// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

const (
	policyAuto       = "auto"
	policyUnitGoTest = "unit-go-test"
)

var errManifestSource = errors.New("exactly one of --range or --labels-file must be provided")

func selectedTargets(ctx context.Context, rangeArg string, command string, scopes []string, policy string) ([]string, error) {
	if policy == "" {
		policy = policyAuto
	}

	switch policy {
	case policyAuto:
		switch command {
		case "test":
			return impactedGoTestManifest(ctx, rangeArg, scopes)
		default:
			return nil, fmt.Errorf("policy %q does not support bazel command %q", policy, command)
		}
	case policyUnitGoTest:
		if command != "test" {
			return nil, fmt.Errorf("policy %q supports only bazel command %q", policy, "test")
		}
		return impactedGoTestManifest(ctx, rangeArg, scopes)
	default:
		return nil, fmt.Errorf("unknown policy %q", policy)
	}
}

func impactedGoTestManifest(ctx context.Context, rangeArg string, scopes []string) ([]string, error) {
	labels, err := impactedLabels(ctx, rangeArg)
	if err != nil {
		return nil, err
	}
	return impactedGoTestManifestFromLabels(ctx, labels, rangeArg, scopes)
}

func impactedGoTestManifestFromLabels(ctx context.Context, labels []string, rangeArg string, scopes []string) ([]string, error) {
	if len(labels) == 0 {
		return nil, nil
	}

	queryDir, cleanup, err := queryDirectoryForRange(ctx, rangeArg)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	testTargets, err := queryScopedGoTests(ctx, queryDir, scopes)
	if err != nil {
		return nil, err
	}
	return filterManifest(labels, testTargets), nil
}

func impactedGoTestManifestFromFile(ctx context.Context, labelsPath string, scopes []string) ([]string, error) {
	labels, err := readLines(labelsPath)
	if err != nil {
		return nil, err
	}
	if len(labels) == 0 {
		return nil, nil
	}

	repoRoot, err := gitOutput(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("resolve repo root: %w", err)
	}

	testTargets, err := queryScopedGoTests(ctx, repoRoot, scopes)
	if err != nil {
		return nil, err
	}
	return filterManifest(labels, testTargets), nil
}

func resolveImpactedLabels(ctx context.Context, rangeArg string, labelsPath string) ([]string, error) {
	switch {
	case rangeArg != "" && labelsPath != "":
		return nil, errManifestSource
	case rangeArg != "":
		return impactedLabels(ctx, rangeArg)
	case labelsPath != "":
		return readLines(labelsPath)
	default:
		return nil, errManifestSource
	}
}

func writeLabelsFile(path string, labels []string) error {
	return os.WriteFile(path, []byte(formatManifest(labels)), 0o600)
}
