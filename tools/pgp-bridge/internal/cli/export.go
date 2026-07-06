// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/tools/pgp-bridge/internal/kms"
)

// newExportCmd builds the `export` subcommand: given an existing KMS key it
// derives the OpenPGP public key (.asc) without creating anything. The key's PGP
// fingerprint is pinned to its KMS CreationDate, so exporting is reproducible.
func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <key-id>",
		Short: "Export an existing KMS key's public part as an OpenPGP public key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			f := cmd.Flags()
			userID, _ := f.GetString(flagUserID)
			if userID == "" {
				return fmt.Errorf("user ID is required (--%s or %s)", flagUserID, envUserID)
			}
			cli, err := buildCLI(ctx, cmd)
			if err != nil {
				return err
			}
			asc, err := cli.Export(ctx, args[0], userID)
			if err != nil {
				return err
			}

			if out, _ := f.GetString(flagOutput); out != "" {
				if err := kms.WriteFileAtomic(out, asc, 0644); err != nil {
					return fmt.Errorf("write public key: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Public key written to: %s\n", out)
				return nil
			}

			n, err := cmd.OutOrStdout().Write(asc)
			if err != nil {
				return err
			}
			if n != len(asc) {
				return io.ErrShortWrite
			}
			return nil
		},
	}
	cmd.Flags().String(flagUserID, envOr(envUserID, ""), "OpenPGP user ID, e.g. \"Name <email>\" (env: "+envUserID+")")
	cmd.Flags().StringP(flagOutput, "o", "", "Write the .asc to this file instead of stdout")
	return cmd
}
