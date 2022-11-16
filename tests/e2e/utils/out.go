// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"fmt"

	"github.com/onsi/ginkgo/v2/formatter"
)

// Outputs to stdout.
//
// e.g.,
//   Outf("{{green}}{{bold}}hi there %q{{/}}", "aa")
//   Outf("{{magenta}}{{bold}}hi therea{{/}} {{cyan}}{{underline}}b{{/}}")
//
// ref.
// https://github.com/onsi/ginkgo/blob/v2.0.0/formatter/formatter.go#L52-L73
//
func Outf(format string, args ...interface{}) {
	s := formatter.F(format, args...)
	fmt.Fprint(formatter.ColorableStdOut, s)
}
