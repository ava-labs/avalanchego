// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

type Level int

const (
	Verbo Level = iota - 9
	Debug
	Trace
	Info
	Warn
	Error
	Fatal
	Off
)

func (l Level) String() string {
	switch l {
	case Off:
		return "OFF"
	case Fatal:
		return "FATAL"
	case Error:
		return "ERROR"
	case Warn:
		return "WARN"
	case Info:
		return "INFO"
	case Trace:
		return "TRACE"
	case Debug:
		return "DEBUG"
	case Verbo:
		return "VERBO"
	default:
		return "UNKNO"
	}
}
