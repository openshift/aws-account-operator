package testutils

import (
	"github.com/go-logr/logr"
)

// LogEntry captures the information about a particular log line.
type LogEntry struct {
	// Error is the error passed to an error log line.
	Error error

	// KeysAndValues are the keys and values that were logged with
	// the log line.
	KeysAndValues []interface{}

	// Level is the level of the info log line.
	Level int

	// Messages is the message from the log line.
	Message string
}

// TestLogger provides a logr.Logger and access to a list of log
// entries logged via the logger.
type TestLogger interface {
	Entries() []LogEntry
	Logger() logr.Logger
}

// NewTestLogger constructs a new TestLogger.
func NewTestLogger() TestLogger {
	l := &testLogger{
		entries: &[]LogEntry{},
	}
	l.logger = logr.New(l)

	return l
}

// testLogger is an implementation of the TestLogger interface.
type testLogger struct {
	entries       *[]LogEntry
	keysAndValues []interface{}
	logger        logr.Logger
	runtimeInfo   logr.RuntimeInfo
}

// Logger provides the TestLoggers logr.Logger.
func (t *testLogger) Logger() logr.Logger {
	return t.logger
}

// Entries returns the previously logged log entries.
func (t *testLogger) Entries() []LogEntry {
	return *t.entries
}

// Init configures the logr.LogSink implementation.
func (t *testLogger) Init(info logr.RuntimeInfo) {
	t.runtimeInfo = info
}

// Enabled is used to determine whether an info log should be logged.
func (t *testLogger) Enabled(_ int) bool {
	// Always return true so that we capture every log line in the test.
	return true
}

// Info accepts an info log line.
func (t *testLogger) Info(level int, msg string, keysAndValues ...interface{}) {
	*t.entries = append(*t.entries, LogEntry{
		KeysAndValues: append(t.keysAndValues, keysAndValues...),
		Level:         level,
		Message:       msg,
	})
}

// Error accepts an error log line.
func (t *testLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	*t.entries = append(*t.entries, LogEntry{
		Error:         err,
		KeysAndValues: append(t.keysAndValues, keysAndValues...),
		Message:       msg,
	})
}

// WithValues creates a child logger with additional keys and values attached.
func (t *testLogger) WithValues(keysAndValues ...interface{}) logr.LogSink {
	return &testLogger{
		runtimeInfo:   t.runtimeInfo,
		logger:        t.logger,
		entries:       t.entries,
		keysAndValues: append(t.keysAndValues, keysAndValues...),
	}
}

// WithName sets the name of the logger. This is not currently supported.
func (t *testLogger) WithName(name string) logr.LogSink {
	return t
}
