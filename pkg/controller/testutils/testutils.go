package testutils

import (
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
