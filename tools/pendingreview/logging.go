// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"io"
	"os"

	"github.com/ava-labs/avalanchego/utils/logging"
)

const logLevelEnv = "GH_PENDING_REVIEW_LOG_LEVEL"

func newLogger(w io.Writer) logging.Logger {
	level := logging.Error
	if configured := os.Getenv(logLevelEnv); configured != "" {
		if parsed, err := logging.ToLevel(configured); err == nil {
			level = parsed
		}
	}
	return logging.NewLogger(
		"pendingreview",
		logging.NewWrappedCore(level, nopWriteCloser{Writer: w}, logging.Plain.ConsoleEncoder()),
	)
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error {
	return nil
}
