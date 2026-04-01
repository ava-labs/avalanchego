package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/tools/draftreview"
)

func main() {
	if err := draftreview.Run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		err = stacktrace.Wrap(err)
		fmt.Fprintln(os.Stderr, err)
		if draftreview.IsUsageError(err) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
