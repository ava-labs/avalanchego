// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"io"

	"github.com/ethereum/go-ethereum/log"

	corethlog "github.com/ava-labs/coreth/log"
)

type CorethLogger struct {
	log.Handler
}

// InitLogger initializes logger with alias and sets the log level and format with the original [os.StdErr] interface
// along with the context logger.
func InitLogger(alias string, level string, jsonFormat bool, writer io.Writer) (CorethLogger, error) {
	logFormat := corethlog.CorethTermFormat(alias)
	if jsonFormat {
		logFormat = corethlog.CorethJSONFormat(alias)
	}

	// Create handler
	logHandler := log.StreamHandler(writer, logFormat)
	c := CorethLogger{Handler: logHandler}

	if err := c.SetLogLevel(level); err != nil {
		return CorethLogger{}, err
	}
	log.PrintOrigins(true)
	return c, nil
}

// SetLogLevel sets the log level of initialized log handler.
func (c *CorethLogger) SetLogLevel(level string) error {
	// Set log level
	logLevel, err := log.LvlFromString(level)
	if err != nil {
		return err
	}
	log.Root().SetHandler(log.LvlFilterHandler(logLevel, c))
	return nil
}
