package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

type StructuredLogger struct {
	level      LogLevel
	output     io.Writer
	mu         sync.Mutex
	jsonFormat bool
	fields     map[string]interface{}
}

type LogEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

func NewStructuredLogger(level LogLevel, jsonFormat bool) *StructuredLogger {
	return &StructuredLogger{
		level:      level,
		output:     os.Stdout,
		jsonFormat: jsonFormat,
		fields:     make(map[string]interface{}),
	}
}

func (sl *StructuredLogger) SetOutput(w io.Writer) {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.output = w
}

func (sl *StructuredLogger) WithFields(fields map[string]interface{}) *StructuredLogger {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	
	newLogger := &StructuredLogger{
		level:      sl.level,
		output:     sl.output,
		jsonFormat: sl.jsonFormat,
		fields:     make(map[string]interface{}),
	}
	
	for k, v := range sl.fields {
		newLogger.fields[k] = v
	}
	
	for k, v := range fields {
		newLogger.fields[k] = v
	}
	
	return newLogger
}

func (sl *StructuredLogger) WithField(key string, value interface{}) *StructuredLogger {
	return sl.WithFields(map[string]interface{}{key: value})
}

func (sl *StructuredLogger) log(level LogLevel, msg string, fields map[string]interface{}) {
	if level < sl.level {
		return
	}
	
	sl.mu.Lock()
	defer sl.mu.Unlock()
	
	allFields := make(map[string]interface{})
	for k, v := range sl.fields {
		allFields[k] = v
	}
	for k, v := range fields {
		allFields[k] = v
	}
	
	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level.String(),
		Message:   msg,
		Fields:    allFields,
	}
	
	if sl.jsonFormat {
		data, err := json.Marshal(entry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to marshal log entry: %v\n", err)
			return
		}
		fmt.Fprintln(sl.output, string(data))
	} else {
		fieldsStr := ""
		if len(allFields) > 0 {
			fieldsBytes, _ := json.Marshal(allFields)
			fieldsStr = " " + string(fieldsBytes)
		}
		fmt.Fprintf(sl.output, "[%s] %s: %s%s\n", entry.Timestamp, entry.Level, msg, fieldsStr)
	}
	
	if level == FATAL {
		os.Exit(1)
	}
}

func (sl *StructuredLogger) Debug(msg string) {
	sl.log(DEBUG, msg, nil)
}

func (sl *StructuredLogger) DebugWithFields(msg string, fields map[string]interface{}) {
	sl.log(DEBUG, msg, fields)
}

func (sl *StructuredLogger) Info(msg string) {
	sl.log(INFO, msg, nil)
}

func (sl *StructuredLogger) InfoWithFields(msg string, fields map[string]interface{}) {
	sl.log(INFO, msg, fields)
}

func (sl *StructuredLogger) Warn(msg string) {
	sl.log(WARN, msg, nil)
}

func (sl *StructuredLogger) WarnWithFields(msg string, fields map[string]interface{}) {
	sl.log(WARN, msg, fields)
}

func (sl *StructuredLogger) Error(msg string) {
	sl.log(ERROR, msg, nil)
}

func (sl *StructuredLogger) ErrorWithFields(msg string, fields map[string]interface{}) {
	sl.log(ERROR, msg, fields)
}

func (sl *StructuredLogger) Fatal(msg string) {
	sl.log(FATAL, msg, nil)
}

func (sl *StructuredLogger) FatalWithFields(msg string, fields map[string]interface{}) {
	sl.log(FATAL, msg, fields)
}

var defaultLogger = NewStructuredLogger(INFO, false)

func SetDefaultLogger(logger *StructuredLogger) {
	defaultLogger = logger
}

func GetDefaultLogger() *StructuredLogger {
	return defaultLogger
}

func Debug(msg string) {
	defaultLogger.Debug(msg)
}

func Info(msg string) {
	defaultLogger.Info(msg)
}

func Warn(msg string) {
	defaultLogger.Warn(msg)
}

func Error(msg string) {
	defaultLogger.Error(msg)
}

func Fatal(msg string) {
	defaultLogger.Fatal(msg)
}

func WithField(key string, value interface{}) *StructuredLogger {
	return defaultLogger.WithField(key, value)
}

func WithFields(fields map[string]interface{}) *StructuredLogger {
	return defaultLogger.WithFields(fields)
}
