package utils

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
