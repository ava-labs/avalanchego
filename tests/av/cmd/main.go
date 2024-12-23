// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

const (
	cmdName        = "av"
	kubectlVersion = "v1.30.2"
)

func main() {
	var rawLogFormat string
	rootCmd := &cobra.Command{
		Use:   cmdName,
		Short: cmdName + " commands",
	}
	rootCmd.PersistentFlags().StringVar(&rawLogFormat, "log-format", logging.AutoString, logging.FormatDescription)

	toolCmd := &cobra.Command{
		Use:   "tool",
		Short: "configured cli tools",
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("please specify a valid tool name")
		},
	}
	rootCmd.AddCommand(toolCmd)

	ginkgoCmd := &cobra.Command{
		Use:   "ginkgo",
		Short: "cli for building and running e2e tests",
		RunE: func(c *cobra.Command, args []string) error {
			cmdArgs := []string{
				"run",
				"github.com/onsi/ginkgo/v2/ginkgo",
			}
			cmdArgs = append(cmdArgs, args...)
			cmd := exec.Command("go", cmdArgs...)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			c.SilenceUsage = true
			c.SilenceErrors = true
			// TODO(marun) Suppress the duplicated 'exit status X'
			// caused by passing through the exit code from the
			// `go run` subcommand
			return cmd.Run()
		},
	}
	toolCmd.AddCommand(ginkgoCmd)

	tmpnetctlCmd := &cobra.Command{
		Use:   "tmpnetctl",
		Short: "cli for managing temporary networks",
		RunE: func(c *cobra.Command, args []string) error {
			cmdArgs := []string{
				"run",
				"github.com/ava-labs/avalanchego/tests/fixture/tmpnet/cmd",
			}
			cmdArgs = append(cmdArgs, args...)
			cmd := exec.Command("go", cmdArgs...)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			c.SilenceUsage = true
			c.SilenceErrors = true
			// TODO(marun) Suppress the duplicated 'exit status X'
			// caused by passing through the exit code from the
			// `go run` subcommand
			return cmd.Run()
		},
	}
	toolCmd.AddCommand(tmpnetctlCmd)

	kubectlCmd := &cobra.Command{
		Use:   "kubectl",
		Short: "cli for interacting with a kubernetes cluster",
		RunE: func(c *cobra.Command, args []string) error {
			log, err := tests.LoggerForFormat("", rawLogFormat)
			if err != nil {
				return err
			}

			downloadDir, err := filepath.Abs(".tool-downloads")
			if err != nil {
				return err
			}

			// Ensure the download directory exists
			if info, err := os.Stat(downloadDir); errors.Is(err, os.ErrNotExist) {
				log.Info("creating tool download directory",
					zap.String("dir", downloadDir),
				)
				if err := os.MkdirAll(downloadDir, perms.ReadWriteExecute); err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if !info.IsDir() {
				return fmt.Errorf("download path is not a directory: %s", downloadDir)
			}

			var (
				kubectlPath = downloadDir + "/kubectl-" + kubectlVersion
				// TODO(marun) Make these dynamic
				kubeOS   = "linux"
				kubeArch = "arm64"
			)

			if _, err := os.Stat(downloadDir); errors.Is(err, os.ErrNotExist) {
				// TODO(marun) Maybe use a library to download the binary?
				curlArgs := []string{
					"curl -L -o " + kubectlPath + " https://dl.k8s.io/release/" + kubectlVersion + "/bin/" + kubeOS + "/" + kubeArch + "/kubectl",
				}
				log.Info("downloading kubectl with curl",
					zap.Strings("curlArgs", curlArgs),
				)
				// Run in a subshell to ensure -L redirects work
				curl := exec.Command("bash", append([]string{"-x", "-c"}, curlArgs...)...)
				curl.Stdin = os.Stdin
				curl.Stdout = os.Stdout
				curl.Stderr = os.Stderr
				c.SilenceUsage = true
				c.SilenceErrors = true
				err = curl.Run()
				if err != nil {
					return err
				}

				if err := os.Chmod(kubectlPath, perms.ReadWriteExecute); err != nil {
					return err
				}

				log.Info("running kubectl for the first time",
					zap.String("path", kubectlPath),
				)
			}

			kubectl := exec.Command(kubectlPath, args...)
			kubectl.Stdin = os.Stdin
			kubectl.Stdout = os.Stdout
			kubectl.Stderr = os.Stderr
			c.SilenceUsage = true
			c.SilenceErrors = true
			return kubectl.Run()
		},
	}
	toolCmd.AddCommand(kubectlCmd)

	if err := rootCmd.Execute(); err != nil {
		var exitCode int = 1
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		}
		os.Exit(exitCode)
	}
	os.Exit(0)
}
