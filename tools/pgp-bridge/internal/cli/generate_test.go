// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// writePolicyFile writes content to a temp file and returns its path.
func writePolicyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write policy file: %v", err)
	}
	return path
}

func TestReadKeyPolicy(t *testing.T) {
	const validPolicy = `{"Version":"2012-10-17","Statement":[]}`

	t.Run("flag omitted returns empty policy", func(t *testing.T) {
		got, err := readKeyPolicy("", false)
		if err != nil {
			t.Fatalf("readKeyPolicy(omitted) error = %v, want nil", err)
		}
		if got != "" {
			t.Errorf("readKeyPolicy(omitted) = %q, want \"\"", got)
		}
	})

	// The deeper regression: an explicitly supplied empty value (--key-policy "")
	// must NOT collapse into the omitted case and fall back to the default policy.
	t.Run("explicit empty value is rejected", func(t *testing.T) {
		if _, err := readKeyPolicy("", true); err == nil {
			t.Error("readKeyPolicy(\"\", provided) error = nil, want non-nil")
		}
	})

	t.Run("valid JSON is returned verbatim", func(t *testing.T) {
		got, err := readKeyPolicy(writePolicyFile(t, validPolicy), true)
		if err != nil {
			t.Fatalf("readKeyPolicy(valid) error = %v, want nil", err)
		}
		if got != validPolicy {
			t.Errorf("readKeyPolicy(valid) = %q, want %q", got, validPolicy)
		}
	})

	t.Run("empty file is rejected", func(t *testing.T) {
		if _, err := readKeyPolicy(writePolicyFile(t, ""), true); err == nil {
			t.Error("readKeyPolicy(empty file) error = nil, want non-nil")
		}
	})

	t.Run("whitespace-only file is rejected", func(t *testing.T) {
		if _, err := readKeyPolicy(writePolicyFile(t, "  \n\t "), true); err == nil {
			t.Error("readKeyPolicy(whitespace file) error = nil, want non-nil")
		}
	})

	t.Run("malformed JSON is rejected", func(t *testing.T) {
		if _, err := readKeyPolicy(writePolicyFile(t, "{not json"), true); err == nil {
			t.Error("readKeyPolicy(malformed JSON) error = nil, want non-nil")
		}
	})

	t.Run("missing file is rejected", func(t *testing.T) {
		if _, err := readKeyPolicy(filepath.Join(t.TempDir(), "nope.json"), true); err == nil {
			t.Error("readKeyPolicy(missing file) error = nil, want non-nil")
		}
	})
}

// TestKeyPolicyFlagChangedSemantics pins the pflag behavior the fix relies on:
// `--key-policy ""` marks the flag as Changed with an empty value, which is how
// the RunE path distinguishes an explicitly empty value from an omitted flag.
func TestKeyPolicyFlagChangedSemantics(t *testing.T) {
	t.Run("omitted flag is not Changed", func(t *testing.T) {
		cmd := parseArgs(t, []string{"generate"})
		if cmd.Flags().Changed("key-policy") {
			t.Error("Changed(key-policy) = true for omitted flag, want false")
		}
	})

	t.Run("explicit empty value is Changed", func(t *testing.T) {
		cmd := parseArgs(t, []string{"generate", "--key-policy", ""})
		f := cmd.Flags()
		if !f.Changed("key-policy") {
			t.Fatal("Changed(key-policy) = false for --key-policy \"\", want true")
		}
		if got, _ := f.GetString("key-policy"); got != "" {
			t.Errorf("GetString(key-policy) = %q, want \"\"", got)
		}
		// The combination Changed==true, value=="" must be rejected, not treated
		// as omitted.
		if _, err := readKeyPolicy("", f.Changed("key-policy")); err == nil {
			t.Error("readKeyPolicy for explicit empty value error = nil, want non-nil")
		}
	})
}
