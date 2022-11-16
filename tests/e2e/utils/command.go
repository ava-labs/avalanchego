// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-cmd/cmd"
)

func RunCommand(ctx context.Context, bin string, args ...string) (cmd.Status, error) {
	Outf("{{green}}running '%s %s'{{/}}\n", bin, strings.Join(args, " "))

	curCmd := cmd.NewCmd(bin, args...)
	statusChan := curCmd.Start()

	// to stream outputs
	ticker := time.NewTicker(10 * time.Millisecond)
	go func() {
		prevLine := ""
		for range ticker.C {
			status := curCmd.Status()
			n := len(status.Stdout)
			if n == 0 {
				continue
			}

			line := status.Stdout[n-1]
			if prevLine != line && line != "" {
				fmt.Println("[streaming output]", line)
			}

			prevLine = line
		}
	}()

	select {
	case s := <-statusChan:
		return s, nil
	case <-ctx.Done():
		curCmd.Stop()
		return cmd.Status{}, ctx.Err()
	}
}
