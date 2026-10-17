// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cli

// Design note: flag names and env var names are declared as explicit, separate
// constants. The pairing (aws-region ↔ AWS_REGION) looks like it could be
// derived via:
//
//	v := viper.New()
//	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
//	v.AutomaticEnv()
//	v.BindPFlags(cmd.Flags())
//	// aws-region → AWS_REGION, no hand-written env constants needed
//
// That is the idiomatic way to collapse these two const blocks into one, and
// the correct move if/when we adopt Viper. We skip it for now: Viper is a
// heavy dependency for a CLI this small, and the no-Viper camp (kubectl, gh)
// treats flags and env vars as separate, explicitly-declared inputs. Do not
// hand-roll a derivation — it just reimplements Viper badly:
//
//	// Don't do this.
//	envName := strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))

// Flag names registered on the command tree. Each is used both when the flag is
// registered and when its value is read back; sharing a constant keeps those
// two sites in lockstep so a rename can never silently break config loading.
//
// Credential flags (access key, secret key, session token) are intentionally
// absent: secrets in argv leak via ps/proc, shell history, and CI logs.
// Credentials come only from the AWS SDK default credential chain.
const (
	// Persistent flags (root command, inherited by subcommands).
	flagAWSRegion = "aws-region"
	flagKMSHost   = "kms-host"

	// Subcommand-local flags.
	flagUserID    = "user-id"
	flagKeyPolicy = "key-policy"
	flagOutput    = "output"
)

// Environment variables that seed the defaults for the flags above.
const (
	envKMSHost = "KMS_HOST"
	envUserID  = "USER_ID"
)
