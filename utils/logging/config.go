// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"fmt"
	"os"
	"time"

	"github.com/chain4travel/caminogo/utils/constants"
	"github.com/chain4travel/caminogo/utils/units"
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
	DisableLogging              bool          `json:"disableLogging"`
	DisableDisplaying           bool          `json:"disableDisplaying"`
	DisableContextualDisplaying bool          `json:"disableContextualDisplaying"`
	DisableFlushOnWrite         bool          `json:"disableFlushOnWrite"`
	Assertions                  bool          `json:"assertions"`
	LogLevel                    Level         `json:"logLevel"`
	DisplayLevel                Level         `json:"displayLevel"`
	DisplayHighlight            Highlight     `json:"displayHighlight"`
	Directory                   string        `json:"directory"`
	MsgPrefix                   string        `json:"-"`
	LoggerName                  string        `json:"-"`
}
