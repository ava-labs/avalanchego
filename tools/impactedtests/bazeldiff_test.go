// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeSRIHash(t *testing.T) {
	t.Parallel()

	decoded, err := decodeSRIHash(bazelDiffHash)
	require.NoError(t, err)
	require.Equal(t, "F7opo1MmvosIVObhv29FA2gq9bBBZfeaPn+96apq5uY=", base64.StdEncoding.EncodeToString(decoded))
}

func TestResolveJavaPathFallsBackToBazelJavaHome(t *testing.T) {
	binDir := t.TempDir()
	javaHome := filepath.Join(t.TempDir(), "jdk")
	javaPath := filepath.Join(javaHome, "bin", "java")
	require.NoError(t, writeExecutableForTest(javaPath, "#!/bin/sh\nexit 0\n"))

	bazelPath := filepath.Join(binDir, "bazel")
	require.NoError(t, writeExecutableForTest(bazelPath, "#!/bin/sh\nprintf 'INFO: Invocation ID: test\\n' >&2\nprintf '%s\\n' \""+javaHome+"\"\n"))

	t.Setenv("PATH", binDir)

	resolvedPath, err := resolveJavaPath(t.Context())
	require.NoError(t, err)
	require.Equal(t, javaPath, resolvedPath)
}

func writeExecutableForTest(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o755)
}
