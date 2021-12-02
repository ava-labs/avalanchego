// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"fmt"
	"time"

	"github.com/mitchellh/go-homedir"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
)

// DefaultLogDirectory is the default directory where logs are saved
var DefaultLogDirectory = fmt.Sprintf("~/.%s/logs", constants.AppName)

// Config defines the configuration of a logger
type Config struct {
	RotationInterval            time.Duration `json:"rotationInterval"`
	FileSize                    int           `json:"fileSize"`
	RotationSize                int           `json:"rotationSize"`
	FlushSize                   int           `json:"flushSize"`
	DisableLogging              bool          `json:"disableLogging"`
	DisableDisplaying           bool          `json:"disableDisplaying"`
	DisableContextualDisplaying bool          `json:"disableContextualDisplaying"`
	DisableFlushOnWrite         bool          `json:"disableFlushOnWrite"`
	Assertions                  bool          `json:"assertions"`
	LogLevel                    Level         `json:"logLevel"`
	DisplayLevel                Level         `json:"displayLevel"`
	DisplayHighlight            Highlight     `json:"displayHighlight"`
	Directory                   string        `json:"-"`
	MsgPrefix                   string        `json:"-"`
	LoggerName                  string        `json:"-"`
}

// DefaultConfig returns a logger configuration with default parameters
func DefaultConfig() (Config, error) {
	dir, err := homedir.Expand(DefaultLogDirectory)
	return Config{
		RotationInterval: 24 * time.Hour,
		FileSize:         8 * units.MiB,
		RotationSize:     7,
		FlushSize:        1,
		DisplayLevel:     Info,
		DisplayHighlight: Plain,
		LogLevel:         Debug,
		Directory:        dir,
	}, err
}
