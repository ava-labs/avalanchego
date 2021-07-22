// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
	RotationInterval                                                                                time.Duration
	FileSize, RotationSize, FlushSize                                                               int
	DisableLogging, DisableDisplaying, DisableContextualDisplaying, DisableFlushOnWrite, Assertions bool
	LogLevel, DisplayLevel                                                                          Level
	DisplayHighlight                                                                                Highlight
	Directory, MsgPrefix, LoggerName                                                                string
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
