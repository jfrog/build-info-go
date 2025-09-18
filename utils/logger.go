package utils

import (
	"log"
	"os"
)

type LevelType int

const (
	ERROR LevelType = iota
	WARN
	INFO
	DEBUG
	VERBOSE
)

type Log interface {
	Debug(a ...interface{})
	Info(a ...interface{})
	Warn(a ...interface{})
	Error(a ...interface{})
	Output(a ...interface{})
	Verbose(a ...interface{})
}

// NullLog is a logger that does nothing
type NullLog struct {
}

func (nl *NullLog) Verbose(...interface{}) {
}

func (nl *NullLog) Debug(...interface{}) {
}

func (nl *NullLog) Info(...interface{}) {
}

func (nl *NullLog) Warn(...interface{}) {
}

func (nl *NullLog) Error(...interface{}) {
}

func (nl *NullLog) Output(...interface{}) {
}

// LoggerAdapter adapts jfrog-client-go logger to build-info-go logger interface
// by implementing the missing Verbose method
type LoggerAdapter struct {
	logger interface {
		Debug(a ...interface{})
		Info(a ...interface{})
		Warn(a ...interface{})
		Error(a ...interface{})
		Output(a ...interface{})
	}
}

// NewLoggerAdapter creates a new adapter for jfrog-client-go loggers
func NewLoggerAdapter(logger interface {
	Debug(a ...interface{})
	Info(a ...interface{})
	Warn(a ...interface{})
	Error(a ...interface{})
	Output(a ...interface{})
}) *LoggerAdapter {
	return &LoggerAdapter{logger: logger}
}

func (la *LoggerAdapter) Verbose(a ...interface{}) {
	// Delegate Verbose calls to Debug to maintain log parsing compatibility
	la.logger.Debug(a...)
}

func (la *LoggerAdapter) Debug(a ...interface{}) {
	la.logger.Debug(a...)
}

func (la *LoggerAdapter) Info(a ...interface{}) {
	la.logger.Info(a...)
}

func (la *LoggerAdapter) Warn(a ...interface{}) {
	la.logger.Warn(a...)
}

func (la *LoggerAdapter) Error(a ...interface{}) {
	la.logger.Error(a...)
}

func (la *LoggerAdapter) Output(a ...interface{}) {
	la.logger.Output(a...)
}

type defaultLogger struct {
	logLevel   LevelType
	outputLog  *log.Logger
	verboseLog *log.Logger
	debugLog   *log.Logger
	infoLog    *log.Logger
	warnLog    *log.Logger
	errorLog   *log.Logger
}

// NewDefaultLogger creates a new logger with a given LogLevel.
// All logs are written to Stderr and output is written to Stdout.
func NewDefaultLogger(logLevel LevelType) *defaultLogger {
	logger := new(defaultLogger)
	logger.logLevel = logLevel
	logger.outputLog = log.New(os.Stdout, "", 0)
	logger.verboseLog = log.New(os.Stderr, "[Verbose] ", 0)
	logger.debugLog = log.New(os.Stderr, "[Debug] ", 0)
	logger.infoLog = log.New(os.Stderr, "[Info] ", 0)
	logger.warnLog = log.New(os.Stderr, "[Warn] ", 0)
	logger.errorLog = log.New(os.Stderr, "[Error] ", 0)
	return logger
}

func (logger defaultLogger) Verbose(a ...interface{}) {
	if logger.logLevel >= VERBOSE {
		logger.verboseLog.Println(a...)
	}
}

func (logger defaultLogger) Debug(a ...interface{}) {
	if logger.logLevel >= DEBUG {
		logger.debugLog.Println(a...)
	}
}

func (logger defaultLogger) Info(a ...interface{}) {
	if logger.logLevel >= INFO {
		logger.infoLog.Println(a...)
	}
}

func (logger defaultLogger) Warn(a ...interface{}) {
	if logger.logLevel >= WARN {
		logger.warnLog.Println(a...)
	}
}

func (logger defaultLogger) Error(a ...interface{}) {
	if logger.logLevel >= ERROR {
		logger.errorLog.Println(a...)
	}
}

func (logger defaultLogger) Output(a ...interface{}) {
	logger.outputLog.Println(a...)
}
