package raft

import (
	"github.com/danthegoodman1/EpicEpoch/gologger"
	dragonlogger "github.com/lni/dragonboat/v3/logger"
	"github.com/rs/zerolog"
)

type RaftGoLogger struct {
	level  dragonlogger.LogLevel
	logger zerolog.Logger
}

// Note that info and debug log levels are stepped down a level

func (r *RaftGoLogger) SetLevel(level dragonlogger.LogLevel) {
	r.level = level
}

func (r *RaftGoLogger) Debugf(format string, args ...interface{}) {
	r.logger.Trace().Msgf(format, args...)
}

func (r *RaftGoLogger) Infof(format string, args ...interface{}) {
	r.logger.Debug().Msgf(format, args...)
}

func (r *RaftGoLogger) Warningf(format string, args ...interface{}) {
	r.logger.Warn().Msgf(format, args...)
}

func (r *RaftGoLogger) Errorf(format string, args ...interface{}) {
	r.logger.Error().Msgf(format, args...)
}

func (r *RaftGoLogger) Panicf(format string, args ...interface{}) {
	r.logger.Panic().Msgf(format, args...)
}

func CreateLogger(name string) dragonlogger.ILogger {
	l := &RaftGoLogger{
		logger: gologger.NewLogger().With().Str("loggerName", name).Str("dragonboat", "y").Logger(),
	}

	return l
}
