// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"fmt"
	"os"
)

type checksFlag []string

func (f *checksFlag) String() string {
	return fmt.Sprint([]string(*f))
}

func (f *checksFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func main() {
	var (
		checks                      checksFlag
		logPath                     string
		allowUncachedGoTestPackages checksFlag
	)
	flag.Var(&checks, "check", "cache check to run; may be repeated")
	flag.StringVar(&logPath, "log", "", "path to the captured job output")
	flag.Var(&allowUncachedGoTestPackages, "allow-uncached-go-test-package", "Go test package allowed to have an uncached result; may be repeated")
	flag.Parse()

	if logPath == "" {
		fmt.Fprintln(os.Stderr, "-log is required")
		os.Exit(2)
	}
	if len(checks) == 0 {
		fmt.Fprintln(os.Stderr, "at least one -check is required")
		os.Exit(2)
	}

	log, err := os.ReadFile(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read log: %v\n", err)
		os.Exit(2)
	}
	if err := checkWithOptions(log, checks, checkOptions{
		allowUncachedGoTestPackages: allowUncachedGoTestPackages,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
