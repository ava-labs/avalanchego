// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	cfgFile     string
	logLevelKey = "log-level"
	cliLog      logging.Logger
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "argo",
	Short: "Simple CLI tool to interact with re-execution",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cli.yaml)")
	rootCmd.PersistentFlags().String(logLevelKey, logging.Info.String(), "Log level")

	rootCmd.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		logLevelStr, err := rootCmd.PersistentFlags().GetString(logLevelKey)
		if err != nil {
			return fmt.Errorf("failed to get %q flag: %w", logLevelKey, err)
		}
		logLevel, err := logging.ToLevel(logLevelStr)
		if err != nil {
			return fmt.Errorf("failed to parse log-level flag %q: %w", logLevelStr, err)
		}
		cliLog = logging.NewLogger(
			"argo",
			logging.NewWrappedCore(
				logLevel,
				os.Stdout,
				logging.Colors.ConsoleEncoder(),
			),
		)
		return nil
	}
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".cli" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".cli")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
