// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"fmt"
	"strings"
)

type Level int

const (
	Off Level = iota
	Fatal
	Error
	Warn
	Info
	Debug
	Verbo
)

func ToLevel(l string) (Level, error) {
	switch strings.ToUpper(l) {
	case "OFF":
		return Off, nil
	case "FATAL":
		return Fatal, nil
	case "ERROR":
		return Error, nil
	case "WARN":
		return Warn, nil
	case "INFO":
		return Info, nil
	case "DEBUG":
		return Debug, nil
	case "VERBO":
		return Verbo, nil
	default:
		return Info, fmt.Errorf("unknown log level: %s", l)
	}
}

func (l Level) Color() Color {
	switch l {
	case Fatal:
		return Red
	case Error:
		return Orange
	case Warn:
		return Yellow
	case Info:
		// Rather than using white, use the default to better support terminals
		// with a white background.
		return Reset
	case Debug:
		return LightBlue
	case Verbo:
		return LightGreen
	default:
		return Reset
	}
}

func (l Level) String() string {
	switch l {
	case Fatal:
		return "FATAL"
	case Error:
		return "ERROR"
	case Warn:
		return "WARN "
	case Info:
		return "INFO "
	case Debug:
		return "DEBUG"
	case Verbo:
		return "VERBO"
	default:
		return "?????"
	}
}
