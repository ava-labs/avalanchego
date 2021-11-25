package utils

import (
	"time"

	"github.com/ethereum/go-ethereum/log"
)

type ProgressLogger struct {
	lastUpdate time.Time
	interval   time.Duration
	logger     log.Logger
}

var _ log.Logger = &ProgressLogger{}

func NewProgressLogger(interval time.Duration) *ProgressLogger {
	return &ProgressLogger{
		lastUpdate: time.Now(),
		interval:   interval,
		logger:     log.Root(),
	}
}

func (pl *ProgressLogger) Trace(msg string, args ...interface{}) {
	if time.Since(pl.lastUpdate) > 15*time.Second {
		pl.logger.Trace(msg, args...)
		pl.lastUpdate = time.Now()
	}
}
func (pl *ProgressLogger) Debug(msg string, args ...interface{}) {
	if time.Since(pl.lastUpdate) > 15*time.Second {
		pl.logger.Debug(msg, args...)
		pl.lastUpdate = time.Now()
	}
}
func (pl *ProgressLogger) Info(msg string, args ...interface{}) {
	if time.Since(pl.lastUpdate) > 15*time.Second {
		pl.logger.Info(msg, args...)
		pl.lastUpdate = time.Now()
	}
}
func (pl *ProgressLogger) Warn(msg string, args ...interface{}) {
	pl.logger.Error(msg, args...)
}
func (pl *ProgressLogger) Error(msg string, args ...interface{}) {
	pl.logger.Error(msg, args...)
}
func (pl *ProgressLogger) Crit(msg string, args ...interface{}) {
	pl.logger.Crit(msg, args...)
}

func (pl *ProgressLogger) New(ctx ...interface{}) log.Logger {
	return &ProgressLogger{
		lastUpdate: pl.lastUpdate,
		interval:   pl.interval,
		logger:     pl.logger.New(ctx...),
	}
}

func (pl *ProgressLogger) GetHandler() log.Handler {
	return pl.logger.GetHandler()
}

func (pl *ProgressLogger) SetHandler(h log.Handler) {
	pl.logger.SetHandler(h)
}
