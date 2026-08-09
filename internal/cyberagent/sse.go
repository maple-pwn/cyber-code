package cyberagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

type EventStream struct {
	body   io.ReadCloser
	reader *bufio.Reader
	limit  int64
	mu     sync.Mutex
	closed bool
}

func (client *Client) Events(ctx context.Context, sessionID string, after *EventCursor) (*EventStream, error) {
	query := url.Values{}
	if after != nil {
		if after.SessionID != sessionID || after.Sequence < 0 || (after.Sequence == 0) != (after.EventID == "") {
			return nil, fmt.Errorf("cyber-agent event cursor is invalid")
		}
		query.Set("after_sequence", strconv.Itoa(after.Sequence))
		if after.EventID != "" {
			query.Set("after_event_id", after.EventID)
		}
	}
	path := sessionPath(sessionID) + "/events"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	request, err := client.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("open cyber-agent event stream: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		payload, readErr := readBounded(response.Body, client.responseLimit)
		if readErr != nil {
			return nil, readErr
		}
		return nil, decodeAPIError(response.StatusCode, payload)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/event-stream" {
		response.Body.Close()
		return nil, fmt.Errorf("cyber-agent event stream returned invalid content type")
	}
	return &EventStream{
		body: response.Body, reader: bufio.NewReader(response.Body), limit: client.responseLimit,
	}, nil
}

func (stream *EventStream) Next(ctx context.Context) (EventEnvelope, error) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed {
		return EventEnvelope{}, io.EOF
	}
	if err := ctx.Err(); err != nil {
		return EventEnvelope{}, err
	}
	cancelled := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = stream.body.Close()
		case <-cancelled:
		}
	}()
	defer close(cancelled)

	event, err := stream.next()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return EventEnvelope{}, ctxErr
	}
	return event, err
}

func (stream *EventStream) next() (EventEnvelope, error) {
	var data []string
	var eventName string
	var eventID string
	var size int64
	for {
		line, err := stream.readLine(&size)
		if err != nil {
			if err == io.EOF && len(data) != 0 {
				return decodeSSEEvent(data, eventName, eventID)
			}
			return EventEnvelope{}, err
		}
		if len(line) == 0 {
			if len(data) == 0 {
				eventName = ""
				eventID = ""
				size = 0
				continue
			}
			return decodeSSEEvent(data, eventName, eventID)
		}
		if line[0] == ':' {
			continue
		}
		field, value, found := strings.Cut(string(line), ":")
		if !found {
			value = ""
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "data":
			data = append(data, value)
		case "event":
			eventName = value
		case "id":
			if !strings.ContainsRune(value, '\x00') {
				eventID = value
			}
		}
	}
}

func (stream *EventStream) readLine(size *int64) ([]byte, error) {
	var line []byte
	for {
		fragment, prefix, err := stream.reader.ReadLine()
		*size += int64(len(fragment))
		if *size > stream.limit {
			return nil, fmt.Errorf("cyber-agent SSE event exceeds %d bytes", stream.limit)
		}
		line = append(line, fragment...)
		if err != nil {
			return nil, err
		}
		if !prefix {
			return line, nil
		}
	}
}

func decodeSSEEvent(data []string, eventName, eventID string) (EventEnvelope, error) {
	payload := []byte(strings.Join(data, "\n"))
	var event EventEnvelope
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return EventEnvelope{}, fmt.Errorf("decode cyber-agent SSE event: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return EventEnvelope{}, fmt.Errorf("decode cyber-agent SSE event: trailing JSON content")
	}
	if event.EventID == "" || event.TaskID == "" || event.SessionID == "" || event.Sequence < 1 {
		return EventEnvelope{}, fmt.Errorf("cyber-agent SSE event requires a session cursor")
	}
	if event.Topic == "" || event.EmittedBy == "" || event.EmittedAt.IsZero() {
		return EventEnvelope{}, fmt.Errorf("cyber-agent SSE event is incomplete")
	}
	if eventID != "" && eventID != event.EventID {
		return EventEnvelope{}, fmt.Errorf("cyber-agent SSE id does not match event payload")
	}
	if eventName != "" && eventName != event.Topic {
		return EventEnvelope{}, fmt.Errorf("cyber-agent SSE topic does not match event payload")
	}
	return event, nil
}

func (stream *EventStream) Close() error {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed {
		return nil
	}
	stream.closed = true
	return stream.body.Close()
}
