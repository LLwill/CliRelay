package logging

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

func (l *FileRequestLogger) writeNonStreamingLog(
	w io.Writer,
	url, method string,
	requestHeaders map[string][]string,
	requestBody []byte,
	requestBodyPath string,
	apiRequest []byte,
	apiResponse []byte,
	apiResponseErrors []*ErrorMessage,
	statusCode int,
	responseHeaders map[string][]string,
	response []byte,
	decompressErr error,
	requestTimestamp time.Time,
	apiResponseTimestamp time.Time,
) error {
	if requestTimestamp.IsZero() {
		requestTimestamp = time.Now()
	}
	if errWrite := writeRequestInfoWithBody(w, url, method, requestHeaders, requestBody, requestBodyPath, requestTimestamp); errWrite != nil {
		return errWrite
	}
	if errWrite := writeAPISection(w, "=== API REQUEST ===\n", "=== API REQUEST", apiRequest, time.Time{}); errWrite != nil {
		return errWrite
	}
	if errWrite := writeAPIErrorResponses(w, apiResponseErrors); errWrite != nil {
		return errWrite
	}
	if errWrite := writeAPISection(w, "=== API RESPONSE ===\n", "=== API RESPONSE", apiResponse, apiResponseTimestamp); errWrite != nil {
		return errWrite
	}
	return writeResponseSection(w, statusCode, true, responseHeaders, bytes.NewReader(response), decompressErr, true)
}

func writeRequestInfoWithBody(
	w io.Writer,
	url, method string,
	headers map[string][]string,
	body []byte,
	bodyPath string,
	timestamp time.Time,
) error {
	if _, errWrite := io.WriteString(w, "=== REQUEST INFO ===\n"); errWrite != nil {
		return errWrite
	}
	if _, errWrite := io.WriteString(w, fmt.Sprintf("Version: %s\n", Version)); errWrite != nil {
		return errWrite
	}
	if _, errWrite := io.WriteString(w, fmt.Sprintf("URL: %s\n", url)); errWrite != nil {
		return errWrite
	}
	if _, errWrite := io.WriteString(w, fmt.Sprintf("Method: %s\n", method)); errWrite != nil {
		return errWrite
	}
	if _, errWrite := io.WriteString(w, fmt.Sprintf("Timestamp: %s\n", timestamp.Format(time.RFC3339Nano))); errWrite != nil {
		return errWrite
	}
	if _, errWrite := io.WriteString(w, "\n"); errWrite != nil {
		return errWrite
	}

	if _, errWrite := io.WriteString(w, "=== HEADERS ===\n"); errWrite != nil {
		return errWrite
	}
	for key, values := range headers {
		for _, value := range values {
			masked := maskSensitiveHeaderValue(key, value)
			if _, errWrite := io.WriteString(w, fmt.Sprintf("%s: %s\n", key, masked)); errWrite != nil {
				return errWrite
			}
		}
	}
	if _, errWrite := io.WriteString(w, "\n"); errWrite != nil {
		return errWrite
	}

	if _, errWrite := io.WriteString(w, "=== REQUEST BODY ===\n"); errWrite != nil {
		return errWrite
	}
	if bodyPath != "" {
		bodyFile, errOpen := os.Open(bodyPath)
		if errOpen != nil {
			return errOpen
		}
		if _, errCopy := io.Copy(w, bodyFile); errCopy != nil {
			_ = bodyFile.Close()
			return errCopy
		}
		if errClose := bodyFile.Close(); errClose != nil {
			log.WithError(errClose).Warn("failed to close request body temp file")
		}
	} else if _, errWrite := w.Write(body); errWrite != nil {
		return errWrite
	}

	if _, errWrite := io.WriteString(w, "\n\n"); errWrite != nil {
		return errWrite
	}
	return nil
}

func writeAPISection(w io.Writer, sectionHeader string, sectionPrefix string, payload []byte, timestamp time.Time) error {
	if len(payload) == 0 {
		return nil
	}

	if bytes.HasPrefix(payload, []byte(sectionPrefix)) {
		if _, errWrite := w.Write(payload); errWrite != nil {
			return errWrite
		}
		if !bytes.HasSuffix(payload, []byte("\n")) {
			if _, errWrite := io.WriteString(w, "\n"); errWrite != nil {
				return errWrite
			}
		}
	} else {
		if _, errWrite := io.WriteString(w, sectionHeader); errWrite != nil {
			return errWrite
		}
		if !timestamp.IsZero() {
			if _, errWrite := io.WriteString(w, fmt.Sprintf("Timestamp: %s\n", timestamp.Format(time.RFC3339Nano))); errWrite != nil {
				return errWrite
			}
		}
		if _, errWrite := w.Write(payload); errWrite != nil {
			return errWrite
		}
		if _, errWrite := io.WriteString(w, "\n"); errWrite != nil {
			return errWrite
		}
	}

	if _, errWrite := io.WriteString(w, "\n"); errWrite != nil {
		return errWrite
	}
	return nil
}

func writeAPIErrorResponses(w io.Writer, apiResponseErrors []*ErrorMessage) error {
	for i := 0; i < len(apiResponseErrors); i++ {
		if apiResponseErrors[i] == nil {
			continue
		}
		if _, errWrite := io.WriteString(w, "=== API ERROR RESPONSE ===\n"); errWrite != nil {
			return errWrite
		}
		if _, errWrite := io.WriteString(w, fmt.Sprintf("HTTP Status: %d\n", apiResponseErrors[i].StatusCode)); errWrite != nil {
			return errWrite
		}
		if apiResponseErrors[i].Error != nil {
			if _, errWrite := io.WriteString(w, apiResponseErrors[i].Error.Error()); errWrite != nil {
				return errWrite
			}
		}
		if _, errWrite := io.WriteString(w, "\n\n"); errWrite != nil {
			return errWrite
		}
	}
	return nil
}

func writeResponseSection(w io.Writer, statusCode int, statusWritten bool, responseHeaders map[string][]string, responseReader io.Reader, decompressErr error, trailingNewline bool) error {
	if _, errWrite := io.WriteString(w, "=== RESPONSE ===\n"); errWrite != nil {
		return errWrite
	}
	if statusWritten {
		if _, errWrite := io.WriteString(w, fmt.Sprintf("Status: %d\n", statusCode)); errWrite != nil {
			return errWrite
		}
	}
	for key, values := range responseHeaders {
		for _, value := range values {
			if _, errWrite := io.WriteString(w, fmt.Sprintf("%s: %s\n", key, value)); errWrite != nil {
				return errWrite
			}
		}
	}
	if _, errWrite := io.WriteString(w, "\n"); errWrite != nil {
		return errWrite
	}
	if responseReader != nil {
		if _, errCopy := io.Copy(w, responseReader); errCopy != nil {
			return errCopy
		}
	}
	if decompressErr != nil {
		if _, errWrite := io.WriteString(w, fmt.Sprintf("\n[DECOMPRESSION ERROR: %v]", decompressErr)); errWrite != nil {
			return errWrite
		}
	}
	if trailingNewline {
		if _, errWrite := io.WriteString(w, "\n"); errWrite != nil {
			return errWrite
		}
	}
	return nil
}

func (l *FileRequestLogger) formatRequestInfo(url, method string, headers map[string][]string, body []byte) string {
	var content strings.Builder
	content.WriteString("=== REQUEST INFO ===\n")
	content.WriteString(fmt.Sprintf("Version: %s\n", Version))
	content.WriteString(fmt.Sprintf("URL: %s\n", url))
	content.WriteString(fmt.Sprintf("Method: %s\n", method))
	content.WriteString(fmt.Sprintf("Timestamp: %s\n", time.Now().Format(time.RFC3339Nano)))
	content.WriteString("\n")

	content.WriteString("=== HEADERS ===\n")
	for key, values := range headers {
		for _, value := range values {
			content.WriteString(fmt.Sprintf("%s: %s\n", key, maskSensitiveHeaderValue(key, value)))
		}
	}
	content.WriteString("\n")
	content.WriteString("=== REQUEST BODY ===\n")
	content.Write(body)
	content.WriteString("\n\n")
	return content.String()
}

func hideAPIKey(apiKey string) string {
	switch {
	case len(apiKey) > 8:
		return apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
	case len(apiKey) > 4:
		return apiKey[:2] + "..." + apiKey[len(apiKey)-2:]
	case len(apiKey) > 2:
		return apiKey[:1] + "..." + apiKey[len(apiKey)-1:]
	default:
		return apiKey
	}
}

func maskAuthorizationHeader(value string) string {
	parts := strings.SplitN(strings.TrimSpace(value), " ", 2)
	if len(parts) < 2 {
		return hideAPIKey(value)
	}
	return parts[0] + " " + hideAPIKey(parts[1])
}

func maskSensitiveHeaderValue(key, value string) string {
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	switch {
	case strings.Contains(lowerKey, "authorization"):
		return maskAuthorizationHeader(value)
	case strings.Contains(lowerKey, "api-key"),
		strings.Contains(lowerKey, "apikey"),
		strings.Contains(lowerKey, "token"),
		strings.Contains(lowerKey, "secret"):
		return hideAPIKey(value)
	default:
		return value
	}
}
