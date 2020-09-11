// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"fmt"
	"time"

	"github.com/mitchellh/go-homedir"

	"github.com/ava-labs/avalanche-go/utils/constants"
)

var (
	// DefaultLogDirectory ...
	DefaultLogDirectory = fmt.Sprintf("~/.%s/logs", constants.AppName)
)

// Config ...
type Config struct {
	RotationInterval                                                                                time.Duration
	FileSize, RotationSize, FlushSize                                                               int
	DisableLogging, DisableDisplaying, DisableContextualDisplaying, DisableFlushOnWrite, Assertions bool
	LogLevel, DisplayLevel                                                                          Level
	DisplayHighlight                                                                                Highlight
	Directory, MsgPrefix                                                                            string
}

// DefaultConfig ...
func DefaultConfig() (Config, error) {
	dir, err := homedir.Expand(DefaultLogDirectory)
	return Config{
		RotationInterval: 24 * time.Hour,
		FileSize:         1 << 23, // 8 MB
		RotationSize:     7,
		FlushSize:        1,
		DisplayLevel:     Info,
		DisplayHighlight: Plain,
		LogLevel:         Debug,
		Directory:        dir,
	}, err
}
