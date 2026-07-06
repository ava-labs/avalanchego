// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cli

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/tools/pgp-bridge/config"
	"github.com/ava-labs/avalanchego/tools/pgp-bridge/internal/kms"
)

// NewRootCmd builds the pgp-bridge command tree: it registers the persistent
// AWS/KMS flags and the generate, sign, and export subcommands.
//
// Returning a freshly constructed tree (rather than a package-level singleton
// wired up in init) keeps the CLI testable: a test can build its own tree, set
// flags or arguments, and inspect the resulting configuration without ever
// reaching AWS KMS.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "pgp-bridge",
		Short:        "Create OpenPGP signatures using AWS KMS as the signing backend",
		SilenceUsage: true,
	}

	pf := root.PersistentFlags()
	// Credentials are NOT accepted as flags: secrets in argv leak via ps/proc,
	// shell history, and CI logs. They come only from the AWS SDK default
	// credential chain (AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY /
	// AWS_SESSION_TOKEN env vars, shared config/credentials files, IAM roles,
	// SSO). Region likewise resolves from AWS_REGION / AWS_DEFAULT_REGION when
	// --aws-region is left unset.
	pf.String(flagAWSRegion, "", "AWS region override; otherwise AWS_REGION / AWS_DEFAULT_REGION, then us-east-1")
	pf.String(flagKMSHost, envOr(envKMSHost, ""), "Custom KMS endpoint host override; defaults to the AWS regional endpoint (env: "+envKMSHost+")")

	root.AddCommand(newGenerateCmd(), newSignCmd(), newExportCmd())
	return root
}

// loadConfig assembles an AWSConfig from the command's already-parsed flags.
// It is the unit-testable seam between Cobra flag parsing and the KMS client:
// given a command with flags set, it deterministically produces the config the
// KMS client will use, with no network or AWS dependency.
func loadConfig(cmd *cobra.Command) config.AWSConfig {
	f := cmd.Flags()
	get := func(name string) string {
		v, _ := f.GetString(name)
		return v
	}
	return config.AWSConfig{
		Host:   get(flagKMSHost),
		Region: get(flagAWSRegion),
	}
}

// buildCLI turns the parsed flags into a ready KMS client. It layers the AWS
// SDK v2-backed constructor over loadConfig's pure flag→config mapping;
// returning an error (rather than a bare struct) lets credential and endpoint
// resolution fail loudly here instead of on the first KMS call.
func buildCLI(ctx context.Context, cmd *cobra.Command) (*kms.CLI, error) {
	return kms.NewCLI(ctx, loadConfig(cmd))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
