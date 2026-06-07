package logging

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

var requestLogID atomic.Uint64

func (l *FileRequestLogger) generateErrorFilename(url string, requestID ...string) string {
	return fmt.Sprintf("error-%s", l.generateFilename(url, requestID...))
}

func (l *FileRequestLogger) ensureLogsDir() error {
	if _, err := os.Stat(l.logsDir); os.IsNotExist(err) {
		return os.MkdirAll(l.logsDir, 0o755)
	}
	return nil
}

func (l *FileRequestLogger) generateFilename(url string, requestID ...string) string {
	path := url
	if strings.Contains(url, "?") {
		path = strings.Split(url, "?")[0]
	}
	path = strings.TrimPrefix(path, "/")
	sanitized := l.sanitizeForFilename(path)
	timestamp := time.Now().Format("2006-01-02T150405")

	var idPart string
	if len(requestID) > 0 && requestID[0] != "" {
		idPart = requestID[0]
	} else {
		idPart = fmt.Sprintf("%d", requestLogID.Add(1))
	}

	return fmt.Sprintf("%s-%s-%s.log", sanitized, timestamp, idPart)
}

func (l *FileRequestLogger) sanitizeForFilename(path string) string {
	sanitized := strings.ReplaceAll(path, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, ":", "-")
	reg := regexp.MustCompile(`[<>:"|?*\s]`)
	sanitized = reg.ReplaceAllString(sanitized, "-")
	reg = regexp.MustCompile(`-+`)
	sanitized = reg.ReplaceAllString(sanitized, "-")
	sanitized = strings.Trim(sanitized, "-")
	if sanitized == "" {
		return "root"
	}
	return sanitized
}

func (l *FileRequestLogger) cleanupOldErrorLogs() error {
	if l.errorLogsMaxFiles <= 0 {
		return nil
	}

	entries, errRead := os.ReadDir(l.logsDir)
	if errRead != nil {
		return errRead
	}

	type logFile struct {
		name    string
		modTime time.Time
	}

	var files []logFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "error-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, errInfo := entry.Info()
		if errInfo != nil {
			log.WithError(errInfo).Warn("failed to read error log info")
			continue
		}
		files = append(files, logFile{name: name, modTime: info.ModTime()})
	}

	if len(files) <= l.errorLogsMaxFiles {
		return nil
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	for _, file := range files[l.errorLogsMaxFiles:] {
		if errRemove := os.Remove(filepath.Join(l.logsDir, file.name)); errRemove != nil {
			log.WithError(errRemove).Warnf("failed to remove old error log: %s", file.name)
		}
	}
	return nil
}

func (l *FileRequestLogger) writeRequestBodyTempFile(body []byte) (string, error) {
	tmpFile, errCreate := os.CreateTemp(l.logsDir, "request-body-*.tmp")
	if errCreate != nil {
		return "", errCreate
	}
	tmpPath := tmpFile.Name()

	if _, errCopy := io.Copy(tmpFile, bytes.NewReader(body)); errCopy != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", errCopy
	}
	if errClose := tmpFile.Close(); errClose != nil {
		_ = os.Remove(tmpPath)
		return "", errClose
	}
	return tmpPath, nil
}
