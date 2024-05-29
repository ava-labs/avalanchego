// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"github.com/onsi/ginkgo/v2/formatter"

	ginkgo "github.com/onsi/ginkgo/v2"
)

// Outputs to stdout.
//
// Examples:
//
//   - Out("{{green}}{{bold}}hi there %q{{/}}", "aa")
//   - Out("{{magenta}}{{bold}}hi therea{{/}} {{cyan}}{{underline}}b{{/}}")
//
// See https://github.com/onsi/ginkgo/blob/v2.0.0/formatter/formatter.go#L52-L73
// for an exhaustive list of color options.
func Outf(format string, args ...interface{}) {
	s := formatter.F(format, args...)
	// Use GinkgoWriter to ensure that output from this function is
	// printed sequentially within other test output produced with
	// GinkgoWriter (e.g. `STEP:...`) when tests are run in
	// parallel. ginkgo collects and writes stdout separately from
	// GinkgoWriter during parallel execution and the resulting output
	// can be confusing.
	ginkgo.GinkgoWriter.Print(s)
}
