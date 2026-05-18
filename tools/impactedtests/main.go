// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("subcommand required")
	}

	switch args[0] {
	case "labels":
		return runLabels(ctx, args[1:])
	case "manifest":
		return runManifest(ctx, args[1:])
	case "select":
		return runSelect(ctx, args[1:])
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: impactedtests <subcommand> [options]

Subcommands:
  labels    Print impacted Bazel labels for a git diff range
  manifest  Print impacted non-manual go_test labels for one or more Bazel scopes
  select    Print selected labels for a Bazel command, scopes, and selection policy

The manifest subcommand can either compute impacted labels from --range or
filter a precomputed impacted-label file via --labels-file.

Examples:
  impactedtests labels --range origin/master..
  impactedtests manifest --range origin/master.. --scope //graft/coreth/... --scope //graft/evm/...
  impactedtests select --range origin/master.. --command test --scope //...
`)
}

func runLabels(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("labels", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	rangeArg := fs.String("range", "", "Git diff range (<base>..<head> or open-ended <base>..)")
	outputPath := fs.String("output", "", "Write output to this file instead of stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rangeArg == "" {
		return errors.New("--range is required")
	}

	labels, err := impactedLabels(ctx, *rangeArg)
	if err != nil {
		return err
	}
	return writeOutput(*outputPath, formatManifest(labels))
}

func runManifest(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("manifest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	rangeArg := fs.String("range", "", "Git diff range (<base>..<head> or open-ended <base>..)")
	labelsPath := fs.String("labels-file", "", "Path to a precomputed impacted-label file to filter instead of recomputing labels")
	var scopes multiFlag
	fs.Var(&scopes, "scope", "Bazel scope/target pattern to search for impacted go_test rules. Repeatable.")
	outputPath := fs.String("output", "", "Write output to this file instead of stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rangeArg == "" && *labelsPath == "" {
		return errors.New("exactly one of --range or --labels-file must be provided")
	}
	if *rangeArg != "" && *labelsPath != "" {
		return errors.New("exactly one of --range or --labels-file must be provided")
	}
	if len(scopes) == 0 {
		return errors.New("at least one --scope is required")
	}

	var (
		manifest []string
		err      error
	)
	if *labelsPath != "" {
		manifest, err = impactedGoTestManifestFromFile(ctx, *labelsPath, scopes)
	} else {
		manifest, err = impactedManifest(ctx, *rangeArg, scopes)
	}
	if err != nil {
		return err
	}
	return writeOutput(*outputPath, formatManifest(manifest))
}

func runSelect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("select", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	rangeArg := fs.String("range", "", "Git diff range (<base>..<head> or open-ended <base>..)")
	command := fs.String("command", "", "Bazel subcommand to select targets for")
	policy := fs.String("policy", policyAuto, "Selection policy (auto, unit-go-test)")
	var scopes multiFlag
	fs.Var(&scopes, "scope", "Bazel scope/target pattern to search within. Repeatable.")
	outputPath := fs.String("output", "", "Write output to this file instead of stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rangeArg == "" {
		return errors.New("--range is required")
	}
	if *command == "" {
		return errors.New("--command is required")
	}
	if len(scopes) == 0 {
		return errors.New("at least one --scope is required")
	}

	selected, err := selectedTargets(ctx, *rangeArg, *command, scopes, *policy)
	if err != nil {
		return err
	}
	return writeOutput(*outputPath, formatManifest(selected))
}

func writeOutput(path string, content string) error {
	if path == "" {
		_, err := os.Stdout.WriteString(content)
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}
