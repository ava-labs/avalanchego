// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	policyAuto       = "auto"
	bazelTestCommand = "test"
)

var errMissingImpactedTestsBin = errors.New("IMPACTEDTESTS_BIN must be set to a Bazel-built impactedtests binary")

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	config, bazelArgs, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 2
	}

	if config.diffRange == "" {
		return execBazel(bazelArgs)
	}
	if config.policy == "" {
		config.policy = policyAuto
	}
	if len(bazelArgs) == 0 {
		fmt.Fprintln(os.Stderr, "ERROR: bazel subcommand required")
		return 2
	}
	if bazelArgs[0] != bazelTestCommand && config.policy == policyAuto {
		fmt.Fprintf(os.Stderr, "WARNING: --diff with policy %q currently supports only %q; passing through %q unchanged\n", config.policy, bazelTestCommand, bazelArgs[0])
		return execBazel(bazelArgs)
	}
	if bazelArgs[0] != bazelTestCommand {
		fmt.Fprintf(os.Stderr, "ERROR: policy %q does not support bazel command %q\n", config.policy, bazelArgs[0])
		return 2
	}

	invocation, err := parseTestInvocation(bazelArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 2
	}
	if len(invocation.targetPatterns) == 0 {
		fmt.Fprintln(os.Stderr, "WARNING: no Bazel target patterns detected; running original command")
		return execBazel(bazelArgs)
	}

	selectedTargets, err := selectTargets(config.diffRange, bazelArgs[0], invocation.targetPatterns, config.policy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: select impacted targets: %v\n", err)
		return 1
	}
	if len(selectedTargets) == 0 {
		fmt.Fprintf(os.Stderr, "No impacted Bazel test targets selected for range %s\n", config.diffRange)
		return 0
	}

	filteredArgs := append([]string{bazelTestCommand}, invocation.preTargetArgs...)
	filteredArgs = append(filteredArgs, selectedTargets...)
	if len(invocation.postDashArgs) > 0 {
		filteredArgs = append(filteredArgs, "--")
		filteredArgs = append(filteredArgs, invocation.postDashArgs...)
	}
	return execBazel(filteredArgs)
}

type config struct {
	diffRange string
	policy    string
}

type testInvocation struct {
	preTargetArgs  []string
	targetPatterns []string
	postDashArgs   []string
}

func parseArgs(args []string) (config, []string, error) {
	cfg := config{}
	for len(args) > 0 {
		switch {
		case args[0] == "--diff":
			if len(args) < 2 {
				return config{}, nil, errors.New("--diff requires a value")
			}
			cfg.diffRange = args[1]
			args = args[2:]
		case strings.HasPrefix(args[0], "--diff="):
			cfg.diffRange = strings.TrimPrefix(args[0], "--diff=")
			args = args[1:]
		case args[0] == "--policy":
			if len(args) < 2 {
				return config{}, nil, errors.New("--policy requires a value")
			}
			cfg.policy = args[1]
			args = args[2:]
		case strings.HasPrefix(args[0], "--policy="):
			cfg.policy = strings.TrimPrefix(args[0], "--policy=")
			args = args[1:]
		default:
			if cfg.diffRange == "" {
				cfg.diffRange = diffRangeFromEnv()
			}
			return cfg, args, nil
		}
	}
	if cfg.diffRange == "" {
		cfg.diffRange = diffRangeFromEnv()
	}
	return cfg, args, nil
}

func diffRangeFromEnv() string {
	if diffRange := os.Getenv("BAZEL_IMPACTED_DIFF_RANGE"); diffRange != "" {
		return diffRange
	}
	if baseSHA := os.Getenv("BAZEL_IMPACTED_BASE_SHA"); baseSHA != "" {
		return baseSHA + ".."
	}
	return ""
}

func parseTestInvocation(args []string) (testInvocation, error) {
	if len(args) == 0 || args[0] != bazelTestCommand {
		return testInvocation{}, errors.New("expected bazel test invocation")
	}

	invocation := testInvocation{}
	args = args[1:]
	postOptions := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if postOptions {
				invocation.postDashArgs = append(invocation.postDashArgs, args[i+1:]...)
				break
			}
			postOptions = true
			continue
		}
		if isTargetPattern(arg) {
			invocation.targetPatterns = append(invocation.targetPatterns, arg)
			continue
		}
		if postOptions {
			return testInvocation{}, fmt.Errorf("unexpected non-target argument %q after end-of-options marker", arg)
		}
		invocation.preTargetArgs = append(invocation.preTargetArgs, arg)
	}
	return invocation, nil
}

func isTargetPattern(arg string) bool {
	return strings.HasPrefix(arg, "//") || strings.HasPrefix(arg, "@") || strings.HasPrefix(arg, "-//")
}

func selectTargets(diffRange string, command string, targetPatterns []string, policy string) ([]string, error) {
	cmd, err := impactedTestsCommand(diffRange, command, targetPatterns, policy)
	if err != nil {
		return nil, err
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, trimmed)
	}
	return splitLines(strings.TrimSpace(string(output))), nil
}

func impactedTestsCommand(diffRange string, command string, targetPatterns []string, policy string) (*exec.Cmd, error) {
	args := make([]string, 0, 7+2*len(targetPatterns))
	args = append(args, "select", "--range", diffRange, "--command", command, "--policy", policy)
	for _, targetPattern := range targetPatterns {
		args = append(args, "--scope", targetPattern)
	}

	if impactedTestsBin := os.Getenv("IMPACTEDTESTS_BIN"); impactedTestsBin != "" {
		return exec.Command(impactedTestsBin, args...), nil
	}

	// TODO(marun): Remove the need for targeted-bazel to shell out to our own
	// impactedtests tool. Until then, Bazel-native callers should provide
	// IMPACTEDTESTS_BIN pointing at a Bazel-built impactedtests binary.
	return nil, errMissingImpactedTestsBin
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func execBazel(args []string) int {
	cmd := exec.Command("bazel", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return exitError.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "ERROR: exec bazel: %v\n", err)
		return 1
	}
	return 0
}
