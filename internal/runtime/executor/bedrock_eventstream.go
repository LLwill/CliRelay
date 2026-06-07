package executor

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func extractBedrockChunkData(payload []byte) []byte {
	b64 := gjson.GetBytes(payload, "bytes").String()
	if b64 == "" {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil
	}
	return decoded
}

func transformBedrockInvocationMetrics(data []byte) []byte {
	metrics := gjson.GetBytes(data, "amazon-bedrock-invocationMetrics")
	if !metrics.Exists() || !metrics.IsObject() {
		return data
	}
	data, _ = sjson.DeleteBytes(data, "amazon-bedrock-invocationMetrics")
	if gjson.GetBytes(data, "usage").Exists() {
		return data
	}
	if inputTokens := metrics.Get("inputTokenCount"); inputTokens.Exists() {
		data, _ = sjson.SetBytes(data, "usage.input_tokens", inputTokens.Int())
	}
	if outputTokens := metrics.Get("outputTokenCount"); outputTokens.Exists() {
		data, _ = sjson.SetBytes(data, "usage.output_tokens", outputTokens.Int())
	}
	return data
}

type bedrockEventStreamDecoder struct {
	reader io.Reader
}

func newBedrockEventStreamDecoder(r io.Reader) *bedrockEventStreamDecoder {
	return &bedrockEventStreamDecoder{reader: r}
}

func (d *bedrockEventStreamDecoder) Decode() ([]byte, error) {
	for {
		prelude := make([]byte, 12)
		if _, err := io.ReadFull(d.reader, prelude); err != nil {
			return nil, err
		}
		expectedPreludeCRC := binary.BigEndian.Uint32(prelude[8:12])
		if crc32.ChecksumIEEE(prelude[:8]) != expectedPreludeCRC {
			return nil, fmt.Errorf("bedrock eventstream: invalid prelude crc")
		}
		totalLen := binary.BigEndian.Uint32(prelude[0:4])
		headersLen := binary.BigEndian.Uint32(prelude[4:8])
		if totalLen < 16 || headersLen > totalLen-16 {
			return nil, fmt.Errorf("bedrock eventstream: invalid frame length")
		}
		rest := make([]byte, int(totalLen)-12)
		if _, err := io.ReadFull(d.reader, rest); err != nil {
			return nil, err
		}
		frameWithoutMessageCRC := append(bytes.Clone(prelude), rest[:len(rest)-4]...)
		expectedMessageCRC := binary.BigEndian.Uint32(rest[len(rest)-4:])
		if crc32.ChecksumIEEE(frameWithoutMessageCRC) != expectedMessageCRC {
			return nil, fmt.Errorf("bedrock eventstream: invalid message crc")
		}
		headers := rest[:headersLen]
		payload := rest[headersLen : len(rest)-4]
		eventHeaders := parseBedrockEventStreamHeaders(headers)
		if eventHeaders.eventType == "chunk" {
			return payload, nil
		}
		if err := bedrockEventStreamException(eventHeaders, payload); err != nil {
			return nil, err
		}
	}
}

type bedrockEventStreamHeaders struct {
	eventType     string
	messageType   string
	exceptionType string
}

func parseBedrockEventStreamHeaders(headers []byte) bedrockEventStreamHeaders {
	out := bedrockEventStreamHeaders{}
	for len(headers) > 0 {
		nameLen := int(headers[0])
		headers = headers[1:]
		if len(headers) < nameLen+3 {
			return out
		}
		name := string(headers[:nameLen])
		headers = headers[nameLen:]
		valueType := headers[0]
		headers = headers[1:]
		if valueType != 7 || len(headers) < 2 {
			return out
		}
		valueLen := int(binary.BigEndian.Uint16(headers[:2]))
		headers = headers[2:]
		if len(headers) < valueLen {
			return out
		}
		value := string(headers[:valueLen])
		headers = headers[valueLen:]
		switch name {
		case ":event-type":
			out.eventType = value
		case ":message-type":
			out.messageType = value
		case ":exception-type":
			out.exceptionType = value
		}
	}
	return out
}

func bedrockEventStreamException(headers bedrockEventStreamHeaders, payload []byte) error {
	eventType := strings.TrimSpace(headers.eventType)
	exceptionType := strings.TrimSpace(headers.exceptionType)
	messageType := strings.TrimSpace(headers.messageType)
	lowerEvent := strings.ToLower(eventType)
	isException := strings.EqualFold(messageType, "exception") ||
		exceptionType != "" ||
		strings.Contains(lowerEvent, "exception") ||
		strings.Contains(lowerEvent, "error")
	if !isException {
		return nil
	}
	errorType := firstNonEmpty(exceptionType, eventType, messageType, "exception")
	message := firstNonEmpty(gjson.GetBytes(payload, "message").String(), gjson.GetBytes(payload, "Message").String(), string(payload))
	return fmt.Errorf("bedrock eventstream %s: %s", errorType, message)
}
