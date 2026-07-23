// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newSignCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sign <key-id>",
		Short: "Sign stdin using the given KMS key, write the signature to stdout",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			message, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
			cli, err := buildCLI(ctx, cmd)
			if err != nil {
				return err
			}
			sig, err := cli.Sign(ctx, args[0], message)
			if err != nil {
				return err
			}
			n, err := cmd.OutOrStdout().Write(sig)
			if err != nil {
				return err
			}
			if n != len(sig) {
				return io.ErrShortWrite
			}
			return nil
		},
	}
}
