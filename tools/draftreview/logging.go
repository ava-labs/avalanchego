// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package draftreview

import (
	"io"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func newDebugLogger(w io.Writer) logging.Logger {
	return logging.NewLogger(
		"draftreview",
		logging.NewWrappedCore(logging.Debug, nopWriteCloser{Writer: w}, logging.Plain.ConsoleEncoder()),
	)
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error {
	return nil
}
