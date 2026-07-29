package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const MaxMessageBytes = 1 << 20

var ErrMessageTooLarge = errors.New("protocol message exceeds size limit")

type Codec struct {
	maxBytes int
}

func NewCodec(maxBytes int) Codec {
	if maxBytes <= 0 || maxBytes > MaxMessageBytes {
		maxBytes = MaxMessageBytes
	}
	return Codec{maxBytes: maxBytes}
}

func (codec Codec) Decode(reader *bufio.Reader, request *Request) error {
	line, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read protocol message: %w", err)
	}
	if len(line) == 0 && errors.Is(err, io.EOF) {
		return io.EOF
	}
	if len(line) > codec.maxBytes {
		return ErrMessageTooLarge
	}
	if json.Unmarshal(line, request) != nil {
		return errors.New("invalid protocol JSON message")
	}
	if request.Version != Version {
		return fmt.Errorf("unsupported protocol version %d", request.Version)
	}
	if request.Type == "" {
		return errors.New("protocol message type is required")
	}
	return nil
}

func (codec Codec) Encode(writer io.Writer, response Response) error {
	response.Version = Version
	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode protocol message: %w", err)
	}
	if len(data) > codec.maxBytes {
		return ErrMessageTooLarge
	}
	data = append(data, '\n')
	_, err = writer.Write(data)
	return err
}
