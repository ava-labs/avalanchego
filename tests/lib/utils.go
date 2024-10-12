// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lib

import (
	"os"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func LoggerForFormat(rawLogFormat string) (logging.Logger, error) {
	writeCloser := os.Stdout
	logFormat, err := logging.ToFormat(rawLogFormat, writeCloser.Fd())
	if err != nil {
		return nil, err
	}
	return logging.NewLogger("", logging.NewWrappedCore(logging.Verbo, writeCloser, logFormat.ConsoleEncoder())), nil
}
