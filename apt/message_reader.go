//  Copyright 2021 Google LLC
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package apt

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var errEmptyMessage = errors.New("empty message")

// MessageReader supports reading Apt messages.
type MessageReader struct {
	reader  *bufio.Reader
	message *Message
}

// NewAptMessageReader returns an AptMessageReader.
func NewAptMessageReader(r *bufio.Reader) *MessageReader {
	return &MessageReader{reader: r}
}

// ReadMessage reads lines from `reader` until a complete message is received.
func (r *MessageReader) ReadMessage(ctx context.Context) (*Message, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		line, err := r.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			if r.message == nil {
				return nil, errEmptyMessage
			}

			// Message is done, return and reset.
			msg := r.message
			r.message = nil
			return msg, nil
		}

		if r.message == nil {
			r.message = &Message{}
			if err := r.parseHeader(line); err != nil {
				return nil, err
			}
		} else {
			if err := r.parseField(line); err != nil {
				return nil, err
			}
		}
	}
}

func (r *MessageReader) parseHeader(line string) error {
	if line == "" {
		return errors.New("empty message header")
	}
	if r.message.code != 0 || r.message.description != "" {
		return errors.New("double parsing header")
	}
	line = strings.TrimSpace(line)
	parts := strings.SplitN(line, " ", 2)
	if len(parts) != 2 {
		return fmt.Errorf("malformed header %q, not enough parts", line)
	}
	code, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return fmt.Errorf("malformed header %q, code is not an integer", line)
	}

	r.message.code = code
	r.message.description = strings.TrimSpace(parts[1])
	return nil
}

func (r *MessageReader) parseField(line string) error {
	if line == "" {
		return errors.New("empty message field")
	}
	line = strings.TrimSpace(line)
	parts := strings.SplitN(line, ":", 2)
	if len(parts) < 2 {
		return fmt.Errorf("malformed field %q, not enough parts", line)
	}
	if r.message.fields == nil {
		r.message.fields = make(map[string][]string)
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if key == "" || value == "" {
		return fmt.Errorf("malformed field %q, empty key or value", line)
	}

	fieldlist := r.message.fields[key]
	fieldlist = append(fieldlist, value)
	r.message.fields[key] = fieldlist
	return nil
}
