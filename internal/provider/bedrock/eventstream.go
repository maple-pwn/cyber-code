package bedrock

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
)

const maxEventStreamFrame = 16 << 20

func transcodeEventStream(source io.ReadCloser) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() {
		err := decodeEventStream(source, writer)
		_ = source.Close()
		_ = writer.CloseWithError(err)
	}()
	return &transcodedBody{PipeReader: reader, source: source}
}

type transcodedBody struct {
	*io.PipeReader
	source io.Closer
}

func (body *transcodedBody) Close() error { _ = body.source.Close(); return body.PipeReader.Close() }

func decodeEventStream(reader io.Reader, output io.Writer) error {
	buffered := bufio.NewReader(reader)
	for {
		prelude := make([]byte, 12)
		if _, err := io.ReadFull(buffered, prelude); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read AWS event prelude: %w", err)
		}
		totalLength := int(binary.BigEndian.Uint32(prelude[0:4]))
		headersLength := int(binary.BigEndian.Uint32(prelude[4:8]))
		if totalLength < 16 || totalLength > maxEventStreamFrame || headersLength > totalLength-16 {
			return fmt.Errorf("invalid AWS event lengths: total=%d headers=%d", totalLength, headersLength)
		}
		if binary.BigEndian.Uint32(prelude[8:12]) != crc32.ChecksumIEEE(prelude[:8]) {
			return fmt.Errorf("AWS event prelude CRC mismatch")
		}
		remainder := make([]byte, totalLength-12)
		if _, err := io.ReadFull(buffered, remainder); err != nil {
			return fmt.Errorf("read AWS event body: %w", err)
		}
		messageWithoutCRC := append(append([]byte(nil), prelude...), remainder[:len(remainder)-4]...)
		if binary.BigEndian.Uint32(remainder[len(remainder)-4:]) != crc32.ChecksumIEEE(messageWithoutCRC) {
			return fmt.Errorf("AWS event message CRC mismatch")
		}
		headers, err := decodeHeaders(remainder[:headersLength])
		if err != nil {
			return err
		}
		payload := remainder[headersLength : len(remainder)-4]
		if headers[":message-type"] != "event" || headers[":event-type"] != "chunk" {
			return fmt.Errorf("unexpected AWS event type %q/%q", headers[":message-type"], headers[":event-type"])
		}
		var chunk struct {
			Bytes string `json:"bytes"`
		}
		if err := json.Unmarshal(payload, &chunk); err != nil {
			return fmt.Errorf("decode Bedrock chunk envelope: %w", err)
		}
		decoded, err := base64.StdEncoding.DecodeString(chunk.Bytes)
		if err != nil {
			return fmt.Errorf("decode Bedrock chunk bytes: %w", err)
		}
		if !json.Valid(decoded) {
			return fmt.Errorf("Bedrock chunk contains invalid JSON")
		}
		if _, err := io.Copy(output, bytes.NewReader(append(append([]byte("data: "), decoded...), '\n', '\n'))); err != nil {
			return err
		}
	}
}

func decodeHeaders(encoded []byte) (map[string]string, error) {
	headers := make(map[string]string)
	for len(encoded) > 0 {
		nameLength := int(encoded[0])
		encoded = encoded[1:]
		if len(encoded) < nameLength+1 {
			return nil, fmt.Errorf("truncated AWS event header")
		}
		name := string(encoded[:nameLength])
		valueType := encoded[nameLength]
		encoded = encoded[nameLength+1:]
		if valueType != 7 || len(encoded) < 2 {
			return nil, fmt.Errorf("unsupported AWS event header %q type %d", name, valueType)
		}
		valueLength := int(binary.BigEndian.Uint16(encoded[:2]))
		encoded = encoded[2:]
		if len(encoded) < valueLength {
			return nil, fmt.Errorf("truncated AWS event header %q", name)
		}
		headers[name] = string(encoded[:valueLength])
		encoded = encoded[valueLength:]
	}
	return headers, nil
}
