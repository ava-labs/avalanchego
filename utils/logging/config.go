// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"fmt"
	"os"
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
)

var (
	homeDir = os.ExpandEnv("$HOME")
	// DefaultLogDirectory is the default directory where logs are saved
	DefaultLogDirectory = fmt.Sprintf("%s/.%s/logs", homeDir, constants.AppName)

	// DefaultConfig provides a reasonable default logger configuration. It
	// should not be modified, it should be copied if changes are intended on
	// being made.
	DefaultConfig = Config{
		RotationInterval: 24 * time.Hour,
		FileSize:         8 * units.MiB,
		RotationSize:     7,
		FlushSize:        1,
		DisplayLevel:     Info,
		DisplayHighlight: Plain,
		LogLevel:         Debug,
		Directory:        DefaultLogDirectory,
	}
)

// Config defines the configuration of a logger
type Config struct {
	RotationInterval            time.Duration `json:"rotationInterval"`
	FileSize                    int           `json:"fileSize"`
	RotationSize                int           `json:"rotationSize"`
	FlushSize                   int           `json:"flushSize"`
	DisableContextualDisplaying bool          `json:"disableContextualDisplaying"`
	DisableFlushOnWrite         bool          `json:"disableFlushOnWrite"`
	DisableWriterDisplaying     bool          `json:"disableWriterDisplaying"`
	Assertions                  bool          `json:"assertions"`
	LogLevel                    Level         `json:"logLevel"`
	DisplayLevel                Level         `json:"displayLevel"`
	DisplayHighlight            Highlight     `json:"displayHighlight"`
	Directory                   string        `json:"directory"`
	MsgPrefix                   string        `json:"-"`
	LoggerName                  string        `json:"-"`
}
