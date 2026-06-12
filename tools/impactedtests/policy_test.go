// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectedTargetsRejectsUnsupportedPolicy(t *testing.T) {
	t.Parallel()

	_, err := selectedTargets(t.Context(), "HEAD..HEAD", "test", []string{"//..."}, "unknown")
	require.EqualError(t, err, `unknown policy "unknown"`)
}

func TestSelectedTargetsRejectsUnsupportedCommandForUnitPolicy(t *testing.T) {
	t.Parallel()

	_, err := selectedTargets(t.Context(), "HEAD..HEAD", "build", []string{"//..."}, policyUnitGoTest)
	require.EqualError(t, err, `policy "unit-go-test" supports only bazel command "test"`)
}

func TestSelectedTargetsRejectsUnsupportedCommandForAutoPolicy(t *testing.T) {
	t.Parallel()

	_, err := selectedTargets(t.Context(), "HEAD..HEAD", "build", []string{"//..."}, policyAuto)
	require.EqualError(t, err, `policy "auto" does not support bazel command "build"`)
}

func TestResolveImpactedLabelsRejectsMissingSource(t *testing.T) {
	t.Parallel()

	_, err := resolveImpactedLabels(t.Context(), "", "")
	require.EqualError(t, err, `exactly one of --range or --labels-file must be provided`)
}

func TestResolveImpactedLabelsRejectsMultipleSources(t *testing.T) {
	t.Parallel()

	_, err := resolveImpactedLabels(t.Context(), "HEAD..HEAD", "labels.txt")
	require.EqualError(t, err, `exactly one of --range or --labels-file must be provided`)
}

func TestResolveImpactedLabelsReadsLabelsFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "labels.txt")
	require.NoError(t, writeLabelsFile(path, []string{"//a:test", "//b:test"}))

	labels, err := resolveImpactedLabels(t.Context(), "", path)
	require.NoError(t, err)
	require.Equal(t, []string{"//a:test", "//b:test"}, labels)
}
