package testutils

import (
	"fmt"

	"github.com/go-logr/logr"
)

type NullLogger struct{}

func (_ NullLogger) Info(_ string, _ ...interface{}) {
	// Do nothing.
}

func (_ NullLogger) Enabled() bool {
	return false
}

func (_ NullLogger) Error(_ error, _ string, _ ...interface{}) {
	// Do nothing.
}

func (log NullLogger) V(_ int) logr.InfoLogger {
	return log
}

func (log NullLogger) WithName(_ string) logr.Logger {
	return log
}

func (log NullLogger) WithValues(_ ...interface{}) logr.Logger {
	return log
}

type TestLogger struct {
	Output []string
}

func (t *TestLogger) doLog(msg string, keysAndValues ...interface{}) {
	t.Output = append(t.Output, fmt.Sprintf("%s %v", msg, keysAndValues))
}

func (t *TestLogger) Enabled() bool {
	return true
}

func (t *TestLogger) Info(msg string, keysAndValues ...interface{}) {
	t.doLog(msg, keysAndValues...)
}

func (t *TestLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	t.doLog(msg, append(keysAndValues, err)...)
}

func (t *TestLogger) V(_ int) logr.InfoLogger {
	return t
}

func (t *TestLogger) WithValues(keysAndValues ...interface{}) logr.Logger {
	return t
}
func (t *TestLogger) WithName(name string) logr.Logger {
	return t
}
