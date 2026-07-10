package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Create a KMS ECDSA P-256 key and export the OpenPGP public key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			f := cmd.Flags()
			userID, _ := f.GetString(flagUserID)
			if userID == "" {
				return fmt.Errorf("user ID is required (--%s or %s)", flagUserID, envUserID)
			}
			policyPath, _ := f.GetString(flagKeyPolicy)
			keyPolicy, err := readKeyPolicy(policyPath, f.Changed(flagKeyPolicy))
			if err != nil {
				return err
			}
			cli, err := buildCLI(ctx, cmd)
			if err != nil {
				return err
			}
			return cli.Generate(ctx, userID, keyPolicy)
		},
	}
	cmd.Flags().String(flagUserID, envOr(envUserID, ""), "OpenPGP user ID, e.g. \"Name <email>\" (env: "+envUserID+")")
	cmd.Flags().String(flagKeyPolicy, "", "Path to a JSON key policy file; applied to the created key (default: AWS default key policy)")
	return cmd
}

// readKeyPolicy loads the --key-policy file. provided reports whether the flag
// was set at all (cmd.Flags().Changed): when it was not, no policy is returned
// and the default KMS key policy applies. The flag value itself is never trusted
// to convey that — an explicitly supplied empty value (--key-policy "") is a
// usage error, not a request for the default, so it is rejected rather than
// silently falling back. When a path IS given the file must be a non-empty,
// well-formed JSON document: an empty, whitespace-only, or malformed file is
// rejected here rather than silently falling back to the default policy, which
// would defeat the purpose of the flag and leave the signing key authorized by
// account IAM alone.
func readKeyPolicy(policyPath string, provided bool) (string, error) {
	if !provided {
		return "", nil
	}
	if policyPath == "" {
		return "", fmt.Errorf("--key-policy was set to an empty path; provide a JSON policy file or omit the flag")
	}
	data, err := os.ReadFile(policyPath)
	if err != nil {
		return "", fmt.Errorf("read key policy file %s: %w", policyPath, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return "", fmt.Errorf("key policy file %s is empty", policyPath)
	}
	if !json.Valid(data) {
		return "", fmt.Errorf("key policy file %s is not valid JSON", policyPath)
	}
	return string(data), nil
}
