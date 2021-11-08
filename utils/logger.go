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
)

type Log interface {
	Debug(a ...interface{})
	Info(a ...interface{})
	Warn(a ...interface{})
	Error(a ...interface{})
	Output(a ...interface{})
}

// NullLog is a logger that does nothing
type NullLog struct {
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

type defaultLogger struct {
	logLevel  LevelType
	outputLog *log.Logger
	debugLog  *log.Logger
	infoLog   *log.Logger
	warnLog   *log.Logger
	errorLog  *log.Logger
}

// NewDefaultLogger creates a new logger with a given LogLevel.
// All logs are written to Stderr and output is written to Stdout.
func NewDefaultLogger(logLevel LevelType) *defaultLogger {
	logger := new(defaultLogger)
	logger.logLevel = logLevel
	logger.outputLog = log.New(os.Stdout, "", 0)
	logger.debugLog = log.New(os.Stderr, "[Debug] ", 0)
	logger.infoLog = log.New(os.Stderr, "[Info] ", 0)
	logger.warnLog = log.New(os.Stderr, "[Warn] ", 0)
	logger.errorLog = log.New(os.Stderr, "[Error] ", 0)
	return logger
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
