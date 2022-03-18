// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"fmt"

	"github.com/onsi/ginkgo/v2/formatter"
)

// Outputs to stdout.
//
// e.g.,
//   Out("{{green}}{{bold}}hi there %q{{/}}", "aa")
//   Out("{{magenta}}{{bold}}hi therea{{/}} {{cyan}}{{underline}}b{{/}}")
//
// ref.
// https://github.com/onsi/ginkgo/blob/v2.0.0/formatter/formatter.go#L52-L73
//
func Outf(format string, args ...interface{}) {
	s := formatter.F(format, args...)
	fmt.Fprint(formatter.ColorableStdOut, s)
}
