// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"io"

	"github.com/ethereum/go-ethereum/log"

	subnetevmlog "github.com/ava-labs/subnet-evm/log"
)

type SubnetEVMLogger struct {
	log.Handler
}

// InitLogger initializes logger with alias and sets the log level and format with the original [os.StdErr] interface
// along with the context logger.
func InitLogger(alias string, level string, jsonFormat bool, writer io.Writer) (SubnetEVMLogger, error) {
	logFormat := subnetevmlog.SubnetEVMTermFormat(alias)
	if jsonFormat {
		logFormat = subnetevmlog.SubnetEVMJSONFormat(alias)
	}

	// Create handler
	logHandler := log.StreamHandler(writer, logFormat)
	c := SubnetEVMLogger{Handler: logHandler}

	if err := c.SetLogLevel(level); err != nil {
		return SubnetEVMLogger{}, err
	}
	log.PrintOrigins(true)
	return c, nil
}

// SetLogLevel sets the log level of initialized log handler.
func (s *SubnetEVMLogger) SetLogLevel(level string) error {
	// Set log level
	logLevel, err := log.LvlFromString(level)
	if err != nil {
		return err
	}
	log.Root().SetHandler(log.LvlFilterHandler(logLevel, s))
	return nil
}
