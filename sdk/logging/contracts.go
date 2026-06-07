package logging

import (
	"net/http"
	"path/filepath"
	"time"
)

const defaultErrorLogsMaxFiles = 10

// Version is included in request log headers for SDK-side log files.
// Release builds can override it via ldflags if needed.
var Version = "dev"

// ErrorMessage captures an upstream error with its associated HTTP metadata.
type ErrorMessage struct {
	StatusCode int
	Error      error
	Addon      http.Header
}

// RequestLogger defines the interface for logging HTTP requests and responses.
type RequestLogger interface {
	LogRequest(url, method string, requestHeaders map[string][]string, body []byte, statusCode int, responseHeaders map[string][]string, response, apiRequest, apiResponse []byte, apiResponseErrors []*ErrorMessage, requestID string, requestTimestamp, apiResponseTimestamp time.Time) error
	LogStreamingRequest(url, method string, headers map[string][]string, body []byte, requestID string) (StreamingLogWriter, error)
	IsEnabled() bool
}

// StreamingLogWriter handles real-time logging of streaming response chunks.
type StreamingLogWriter interface {
	WriteChunkAsync(chunk []byte)
	WriteStatus(status int, headers map[string][]string) error
	WriteAPIRequest(apiRequest []byte) error
	WriteAPIResponse(apiResponse []byte) error
	SetFirstChunkTimestamp(timestamp time.Time)
	Close() error
}

// FileRequestLogger implements RequestLogger using file-based storage.
type FileRequestLogger struct {
	enabled           bool
	logsDir           string
	errorLogsMaxFiles int
}

func newFileRequestLoggerWithOptions(enabled bool, logsDir string, configDir string, errorLogsMaxFiles int) *FileRequestLogger {
	if !filepath.IsAbs(logsDir) && configDir != "" {
		logsDir = filepath.Join(configDir, logsDir)
	}
	return &FileRequestLogger{
		enabled:           enabled,
		logsDir:           logsDir,
		errorLogsMaxFiles: errorLogsMaxFiles,
	}
}

// NewFileRequestLogger creates a new file-based request logger with default error log retention (10 files).
func NewFileRequestLogger(enabled bool, logsDir string, configDir string) *FileRequestLogger {
	return newFileRequestLoggerWithOptions(enabled, logsDir, configDir, defaultErrorLogsMaxFiles)
}

// NewFileRequestLoggerWithOptions creates a new file-based request logger with configurable error log retention.
func NewFileRequestLoggerWithOptions(enabled bool, logsDir string, configDir string, errorLogsMaxFiles int) *FileRequestLogger {
	return newFileRequestLoggerWithOptions(enabled, logsDir, configDir, errorLogsMaxFiles)
}
