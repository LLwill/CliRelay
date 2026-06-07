package logging

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	log "github.com/sirupsen/logrus"
)

func (l *FileRequestLogger) decompressResponse(responseHeaders map[string][]string, response []byte) ([]byte, error) {
	if responseHeaders == nil || len(response) == 0 {
		return response, nil
	}

	var contentEncoding string
	for key, values := range responseHeaders {
		if strings.ToLower(key) == "content-encoding" && len(values) > 0 {
			contentEncoding = strings.ToLower(values[0])
			break
		}
	}

	switch contentEncoding {
	case "gzip":
		return l.decompressGzip(response)
	case "deflate":
		return l.decompressDeflate(response)
	case "br":
		return l.decompressBrotli(response)
	case "zstd":
		return l.decompressZstd(response)
	default:
		return response, nil
	}
}

func (l *FileRequestLogger) decompressGzip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() {
		if errClose := reader.Close(); errClose != nil {
			log.WithError(errClose).Warn("failed to close gzip reader in request logger")
		}
	}()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress gzip data: %w", err)
	}
	return decompressed, nil
}

func (l *FileRequestLogger) decompressDeflate(data []byte) ([]byte, error) {
	reader := flate.NewReader(bytes.NewReader(data))
	defer func() {
		if errClose := reader.Close(); errClose != nil {
			log.WithError(errClose).Warn("failed to close deflate reader in request logger")
		}
	}()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress deflate data: %w", err)
	}
	return decompressed, nil
}

func (l *FileRequestLogger) decompressBrotli(data []byte) ([]byte, error) {
	reader := brotli.NewReader(bytes.NewReader(data))
	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress brotli data: %w", err)
	}
	return decompressed, nil
}

func (l *FileRequestLogger) decompressZstd(data []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd reader: %w", err)
	}
	defer decoder.Close()

	decompressed, err := io.ReadAll(decoder)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress zstd data: %w", err)
	}
	return decompressed, nil
}
