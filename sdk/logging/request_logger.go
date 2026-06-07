// Package logging provides request logging functionality for the CLI Proxy API server.
//
// Request logs are split between non-streaming and streaming paths. Streaming
// responses spool body chunks to temporary files first so long-lived responses
// do not need to stay resident in memory while the request is in flight.
package logging

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
)

func (l *FileRequestLogger) IsEnabled() bool {
	return l.enabled
}

func (l *FileRequestLogger) SetEnabled(enabled bool) {
	l.enabled = enabled
}

func (l *FileRequestLogger) SetErrorLogsMaxFiles(maxFiles int) {
	l.errorLogsMaxFiles = maxFiles
}

func (l *FileRequestLogger) LogRequest(url, method string, requestHeaders map[string][]string, body []byte, statusCode int, responseHeaders map[string][]string, response, apiRequest, apiResponse []byte, apiResponseErrors []*ErrorMessage, requestID string, requestTimestamp, apiResponseTimestamp time.Time) error {
	return l.logRequest(url, method, requestHeaders, body, statusCode, responseHeaders, response, apiRequest, apiResponse, apiResponseErrors, false, requestID, requestTimestamp, apiResponseTimestamp)
}

// LogRequestWithOptions keeps forced error-log writes available even when
// regular request logging is disabled.
func (l *FileRequestLogger) LogRequestWithOptions(url, method string, requestHeaders map[string][]string, body []byte, statusCode int, responseHeaders map[string][]string, response, apiRequest, apiResponse []byte, apiResponseErrors []*ErrorMessage, force bool, requestID string, requestTimestamp, apiResponseTimestamp time.Time) error {
	return l.logRequest(url, method, requestHeaders, body, statusCode, responseHeaders, response, apiRequest, apiResponse, apiResponseErrors, force, requestID, requestTimestamp, apiResponseTimestamp)
}

func (l *FileRequestLogger) logRequest(url, method string, requestHeaders map[string][]string, body []byte, statusCode int, responseHeaders map[string][]string, response, apiRequest, apiResponse []byte, apiResponseErrors []*ErrorMessage, force bool, requestID string, requestTimestamp, apiResponseTimestamp time.Time) error {
	if !l.enabled && !force {
		return nil
	}
	if errEnsure := l.ensureLogsDir(); errEnsure != nil {
		return fmt.Errorf("failed to create logs directory: %w", errEnsure)
	}

	filename := l.generateFilename(url, requestID)
	if force && !l.enabled {
		filename = l.generateErrorFilename(url, requestID)
	}
	filePath := filepath.Join(l.logsDir, filename)

	requestBodyPath, errTemp := l.writeRequestBodyTempFile(body)
	if errTemp != nil {
		log.WithError(errTemp).Warn("failed to create request body temp file, falling back to direct write")
	}
	if requestBodyPath != "" {
		defer func() {
			if errRemove := os.Remove(requestBodyPath); errRemove != nil {
				log.WithError(errRemove).Warn("failed to remove request body temp file")
			}
		}()
	}

	responseToWrite, decompressErr := l.decompressResponse(responseHeaders, response)
	if decompressErr != nil {
		responseToWrite = response
	}

	logFile, errOpen := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if errOpen != nil {
		return fmt.Errorf("failed to create log file: %w", errOpen)
	}

	writeErr := l.writeNonStreamingLog(
		logFile,
		url,
		method,
		requestHeaders,
		body,
		requestBodyPath,
		apiRequest,
		apiResponse,
		apiResponseErrors,
		statusCode,
		responseHeaders,
		responseToWrite,
		decompressErr,
		requestTimestamp,
		apiResponseTimestamp,
	)
	if errClose := logFile.Close(); errClose != nil {
		log.WithError(errClose).Warn("failed to close request log file")
		if writeErr == nil {
			return errClose
		}
	}
	if writeErr != nil {
		return fmt.Errorf("failed to write log file: %w", writeErr)
	}

	if force && !l.enabled {
		if errCleanup := l.cleanupOldErrorLogs(); errCleanup != nil {
			log.WithError(errCleanup).Warn("failed to clean up old error logs")
		}
	}

	return nil
}

// LogStreamingRequest creates a streaming writer that buffers request metadata
// immediately and spools response chunks asynchronously.
func (l *FileRequestLogger) LogStreamingRequest(url, method string, headers map[string][]string, body []byte, requestID string) (StreamingLogWriter, error) {
	if !l.enabled {
		return &NoOpStreamingLogWriter{}, nil
	}
	if err := l.ensureLogsDir(); err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	filename := l.generateFilename(url, requestID)
	filePath := filepath.Join(l.logsDir, filename)

	requestHeaders := make(map[string][]string, len(headers))
	for key, values := range headers {
		headerValues := make([]string, len(values))
		copy(headerValues, values)
		requestHeaders[key] = headerValues
	}

	requestBodyPath, errTemp := l.writeRequestBodyTempFile(body)
	if errTemp != nil {
		return nil, fmt.Errorf("failed to create request body temp file: %w", errTemp)
	}

	responseBodyFile, errCreate := os.CreateTemp(l.logsDir, "response-body-*.tmp")
	if errCreate != nil {
		_ = os.Remove(requestBodyPath)
		return nil, fmt.Errorf("failed to create response body temp file: %w", errCreate)
	}
	responseBodyPath := responseBodyFile.Name()

	writer := &FileStreamingLogWriter{
		logFilePath:      filePath,
		url:              url,
		method:           method,
		timestamp:        time.Now(),
		requestHeaders:   requestHeaders,
		requestBodyPath:  requestBodyPath,
		responseBodyPath: responseBodyPath,
		responseBodyFile: responseBodyFile,
		chunkChan:        make(chan []byte, 100),
		closeChan:        make(chan struct{}),
		errorChan:        make(chan error, 1),
	}

	go writer.asyncWriter()
	return writer, nil
}

// formatLogContent remains as a lightweight formatter for tests and any future
// callers that need the non-streaming text representation without hitting disk.
func (l *FileRequestLogger) formatLogContent(url, method string, headers map[string][]string, body, apiRequest, apiResponse, response []byte, status int, responseHeaders map[string][]string, apiResponseErrors []*ErrorMessage) string {
	var content bytes.Buffer
	content.WriteString(l.formatRequestInfo(url, method, headers, body))

	if len(apiRequest) > 0 {
		if bytes.HasPrefix(apiRequest, []byte("=== API REQUEST")) {
			content.Write(apiRequest)
			if !bytes.HasSuffix(apiRequest, []byte("\n")) {
				content.WriteString("\n")
			}
		} else {
			content.WriteString("=== API REQUEST ===\n")
			content.Write(apiRequest)
			content.WriteString("\n")
		}
		content.WriteString("\n")
	}

	for i := 0; i < len(apiResponseErrors); i++ {
		if apiResponseErrors[i] == nil {
			continue
		}
		content.WriteString("=== API ERROR RESPONSE ===\n")
		content.WriteString(fmt.Sprintf("HTTP Status: %d\n", apiResponseErrors[i].StatusCode))
		if apiResponseErrors[i].Error != nil {
			content.WriteString(apiResponseErrors[i].Error.Error())
		}
		content.WriteString("\n\n")
	}

	if len(apiResponse) > 0 {
		if bytes.HasPrefix(apiResponse, []byte("=== API RESPONSE")) {
			content.Write(apiResponse)
			if !bytes.HasSuffix(apiResponse, []byte("\n")) {
				content.WriteString("\n")
			}
		} else {
			content.WriteString("=== API RESPONSE ===\n")
			content.Write(apiResponse)
			content.WriteString("\n")
		}
		content.WriteString("\n")
	}

	content.WriteString("=== RESPONSE ===\n")
	content.WriteString(fmt.Sprintf("Status: %d\n", status))
	for key, values := range responseHeaders {
		for _, value := range values {
			content.WriteString(fmt.Sprintf("%s: %s\n", key, value))
		}
	}
	content.WriteString("\n")
	content.Write(response)
	content.WriteString("\n")

	return content.String()
}
