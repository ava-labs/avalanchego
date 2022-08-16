// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"io"

	"github.com/ethereum/go-ethereum/log"
)

type SubnetEVMLogger struct {
	log.Handler
}

// InitLogger initializes logger with alias and sets the log level and format with the original [os.StdErr] interface
// along with the context logger.
func InitLogger(alias string, level string, jsonFormat bool, writer io.Writer) (SubnetEVMLogger, error) {
	logFormat := SubnetEVMTermFormat(alias)
	if jsonFormat {
		logFormat = SubnetEVMJSONFormat(alias)
	}

	// Create handler
	logHandler := log.StreamHandler(writer, logFormat)
	c := SubnetEVMLogger{Handler: logHandler}

	if err := c.SetLogLevel(level); err != nil {
		return SubnetEVMLogger{}, err
	}
	return c, nil
}

// SetLogLevel sets the log level of initialized log handler.
func (c *SubnetEVMLogger) SetLogLevel(level string) error {
	// Set log level
	logLevel, err := log.LvlFromString(level)
	if err != nil {
		return err
	}
	log.Root().SetHandler(log.LvlFilterHandler(logLevel, c))
	return nil
}

func SubnetEVMTermFormat(alias string) log.Format {
	prefix := fmt.Sprintf("<%s Chain>", alias)
	return log.FormatFunc(func(r *log.Record) []byte {
		location := fmt.Sprintf("%+v", r.Call)
		newMsg := fmt.Sprintf("%s %s: %s", prefix, location, r.Msg)
		r.Msg = newMsg
		return log.TerminalFormat(false).Format(r)
	})
}

func SubnetEVMJSONFormat(alias string) log.Format {
	prefix := fmt.Sprintf("%s Chain", alias)
	return log.FormatFunc(func(r *log.Record) []byte {
		location := fmt.Sprintf("%+v", r.Call)
		r.KeyNames.Lvl = "level"
		r.KeyNames.Time = "timestamp"
		r.Ctx = append(r.Ctx, "logger", prefix)
		r.Ctx = append(r.Ctx, "caller", location)

		return log.JSONFormat().Format(r)
	})
}
