package cli

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/ava-labs/pgp-bridge/config"
)

// parseArgs builds a fresh command tree, resolves the target subcommand for the
// given argv, and parses its flags — exactly what Cobra does before invoking a
// command's RunE, but stopping short of running it. This lets us exercise the
// flag→config logic without touching AWS KMS.
func parseArgs(t *testing.T, argv []string) *cobra.Command {
	t.Helper()
	root := NewRootCmd()
	cmd, remaining, err := root.Find(argv)
	if err != nil {
		t.Fatalf("Find(%v): %v", argv, err)
	}
	if err := cmd.ParseFlags(remaining); err != nil {
		t.Fatalf("ParseFlags(%v): %v", remaining, err)
	}
	return cmd
}

func TestLoadConfigFromFlags(t *testing.T) {
	// Credentials are no longer flags; loadConfig only surfaces the region and
	// KMS host overrides.
	cmd := parseArgs(t, []string{
		"generate",
		"--aws-region", "eu-west-1",
		"--kms-host", "localhost:4566",
	})

	got := loadConfig(cmd)
	want := config.AWSConfig{
		Host:   "localhost:4566",
		Region: "eu-west-1",
	}
	if got != want {
		t.Fatalf("loadConfig() = %+v, want %+v", got, want)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	// Clear the environment so the persistent-flag defaults are what we assert.
	for _, k := range []string{"AWS_REGION", "KMS_HOST"} {
		t.Setenv(k, "")
	}

	got := loadConfig(parseArgs(t, []string{"generate"}))

	// Both default to empty: the AWS SDK resolves the region (and, from it, the
	// regional KMS endpoint) from its own default chain when these are unset.
	if got.Region != "" {
		t.Errorf("default Region = %q, want %q", got.Region, "")
	}
	if got.Host != "" {
		t.Errorf("default Host = %q, want %q", got.Host, "")
	}
}

func TestRegionFlagOnlyNoEnvPrefill(t *testing.T) {
	t.Setenv("AWS_REGION", "ap-southeast-2")

	// loadConfig must NOT prefill the region from the environment: leaving it
	// empty lets the AWS SDK resolve AWS_REGION / AWS_DEFAULT_REGION itself,
	// instead of a hardcoded default silently shadowing AWS_DEFAULT_REGION.
	got := loadConfig(parseArgs(t, []string{"generate"}))
	if got.Region != "" {
		t.Errorf("unset --aws-region Region = %q, want %q (deferred to SDK)", got.Region, "")
	}

	// An explicit flag is the only value loadConfig surfaces.
	got = loadConfig(parseArgs(t, []string{"generate", "--aws-region", "eu-west-1"}))
	if got.Region != "eu-west-1" {
		t.Errorf("flag Region = %q, want %q", got.Region, "eu-west-1")
	}
}

func TestKMSHostEnvFallbackOverriddenByFlag(t *testing.T) {
	t.Setenv("KMS_HOST", "kms.local:4566")

	// No --kms-host flag: the env-derived default should win.
	got := loadConfig(parseArgs(t, []string{"generate"}))
	if got.Host != "kms.local:4566" {
		t.Errorf("env fallback Host = %q, want %q", got.Host, "kms.local:4566")
	}

	// Explicit flag must override the env-derived default.
	got = loadConfig(parseArgs(t, []string{"generate", "--kms-host", "localhost:9999"}))
	if got.Host != "localhost:9999" {
		t.Errorf("flag override Host = %q, want %q", got.Host, "localhost:9999")
	}
}
