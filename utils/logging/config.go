// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

type RotatingWriterConfig struct {
	MaxSize   int    `json:"maxSize"` // in megabytes
	MaxFiles  int    `json:"maxFiles"`
	MaxAge    int    `json:"maxAge"` // in days
	Directory string `json:"directory"`
	Compress  bool   `json:"compress"`
}

// Config defines the configuration of a logger
type Config struct {
	RotatingWriterConfig
	DisableWriterDisplaying bool   `json:"disableWriterDisplaying"`
	LogLevel                Level  `json:"logLevel"`
	DisplayLevel            Level  `json:"displayLevel"`
	LogFormat               Format `json:"logFormat"`
	MsgPrefix               string `json:"-"`
	LoggerName              string `json:"-"`
}
