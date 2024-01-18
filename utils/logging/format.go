// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap/zapcore"
	"golang.org/x/term"
)

// Format modes available
const (
	Plain Format = iota
	Colors
	JSON

	termTimeFormat = "[01-02|15:04:05.000]"
)

var (
	errUnknownFormat = errors.New("unknown format")

	defaultEncoderConfig = zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	jsonEncoderConfig zapcore.EncoderConfig

	termTimeEncoder = zapcore.TimeEncoderOfLayout(termTimeFormat)
)

func init() {
	jsonEncoderConfig = defaultEncoderConfig
	jsonEncoderConfig.EncodeLevel = jsonLevelEncoder
	jsonEncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	jsonEncoderConfig.EncodeDuration = zapcore.NanosDurationEncoder
}

// Highlight mode to apply to displayed logs
type Format int

// ToFormat chooses a highlighting mode
func ToFormat(h string, fd uintptr) (Format, error) {
	switch strings.ToUpper(h) {
	case "PLAIN":
		return Plain, nil
	case "COLORS":
		return Colors, nil
	case "JSON":
		return JSON, nil
	case "AUTO":
		if !term.IsTerminal(int(fd)) {
			return Plain, nil
		}
		return Colors, nil
	default:
		return Plain, fmt.Errorf("unknown format mode: %s", h)
	}
}

func (f Format) MarshalJSON() ([]byte, error) {
	switch f {
	case Plain:
		return []byte(`"PLAIN"`), nil
	case Colors:
		return []byte(`"COLORS"`), nil
	case JSON:
		return []byte(`"JSON"`), nil
	default:
		return nil, errUnknownFormat
	}
}

func (f Format) WrapPrefix(prefix string) string {
	if prefix == "" || f == JSON {
		return prefix
	}
	return fmt.Sprintf("<%s>", prefix)
}

func (f Format) ConsoleEncoder() zapcore.Encoder {
	switch f {
	case Colors:
		return zapcore.NewConsoleEncoder(newTermEncoderConfig(consoleColorLevelEncoder))
	case JSON:
		return zapcore.NewJSONEncoder(jsonEncoderConfig)
	default:
		return zapcore.NewConsoleEncoder(newTermEncoderConfig(levelEncoder))
	}
}

func (f Format) FileEncoder() zapcore.Encoder {
	switch f {
	case JSON:
		return zapcore.NewJSONEncoder(jsonEncoderConfig)
	default:
		return zapcore.NewConsoleEncoder(newTermEncoderConfig(levelEncoder))
	}
}

func newTermEncoderConfig(lvlEncoder zapcore.LevelEncoder) zapcore.EncoderConfig {
	config := defaultEncoderConfig
	config.EncodeLevel = lvlEncoder
	config.EncodeTime = termTimeEncoder
	config.ConsoleSeparator = " "
	return config
}

func levelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(Level(l).String())
}

func jsonLevelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(Level(l).LowerString())
}

func consoleColorLevelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	s, ok := levelToCapitalColorString[Level(l)]
	if !ok {
		s = unknownLevelColor.Wrap(l.String())
	}
	enc.AppendString(s)
}
