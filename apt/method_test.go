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
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHandleConfigure(t *testing.T) {
	var tests = []struct {
		configItems []string
		expected    aptMethodConfig
	}{
		{
			[]string{
				"Acquire::gar::Service-Account-JSON=/path/to/creds.json",
				"Acquire::gar::Service-Account-Email=email-address@domain",
			},
			aptMethodConfig{serviceAccountJSON: "/path/to/creds.json"},
		},
		{
			[]string{
				"Acquire::gar::Service-Account-Email=email-address@domain",
			},
			aptMethodConfig{serviceAccountEmail: "email-address@domain"},
		},
		{
			[]string{
				"some::other::config=value",
			},
			aptMethodConfig{},
		},
		{
			[]string{
				"Debug::Acquire::gar=1",
			},
			aptMethodConfig{debug: true},
		},
		{
			[]string{
				"Debug::Acquire::gar=enable",
			},
			aptMethodConfig{debug: true},
		},
		{
			[]string{
				"Debug::Acquire::gar=10",
			},
			aptMethodConfig{debug: false},
		},
		{
			[]string{
				"Debug::Acquire::gar=0",
			},
			aptMethodConfig{debug: false},
		},
		{
			[]string{
				"Debug::Acquire::gar=-1",
			},
			aptMethodConfig{debug: false},
		},
	}

	for _, tt := range tests {
		method := &Method{config: &aptMethodConfig{}}
		msg := &Message{
			code:        601,
			description: "Configuration",
			fields:      map[string][]string{"Config-Item": tt.configItems},
		}

		method.handleConfigure(msg)
		if method.config.serviceAccountJSON != tt.expected.serviceAccountJSON {
			t.Errorf("path config items don't match, got %q expected %q", method.config.serviceAccountJSON, tt.expected.serviceAccountJSON)
		}
		if method.config.serviceAccountEmail != tt.expected.serviceAccountEmail {
			t.Errorf("email config items don't match, got %q expected %q", method.config.serviceAccountEmail, tt.expected.serviceAccountEmail)
		}

	}

}

type fakeHTTPClient struct {
	code   int
	header map[string][]string
}

func (m fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.code == 0 {
		m.code = 200
	}
	if m.header == nil {
		m.header = map[string][]string{"Content-Length": {"200"}, "Last-Modified": {"whenever"}}
	}
	return &http.Response{StatusCode: m.code, Header: m.header}, nil
}

type fakeDownloader struct{}

func (d fakeDownloader) download(_ io.ReadCloser, _ string) (string, error) {
	return "ABCDEFGHI", nil
}

func TestAptMethodRun(t *testing.T) {

	stdinreader, stdinwriter := io.Pipe()
	stdoutreader, stdoutwriter := io.Pipe()
	workMethod := NewAptMethod(bufio.NewReader(stdinreader), stdoutwriter)
	workMethod.client = fakeHTTPClient{}
	workMethod.dl = fakeDownloader{}

	ctx := context.Background()
	ctx2, cancel := context.WithCancel(ctx)
	go workMethod.Run(ctx2)

	reader := MessageReader{reader: bufio.NewReader(stdoutreader)}
	msg, err := reader.ReadMessage(ctx)
	if err != nil {
		t.Errorf("failed, %v", err)
	}
	if msg.code != 100 || msg.description != "Capabilities" {
		t.Errorf("failed, didn't receive capabilities message")
	}

	writer := MessageWriter{writer: stdinwriter}
	writer.WriteMessage(Message{
		code:        601,
		description: "Configuration",
		fields:      map[string][]string{"Config-Item": {"Acquire::gar::Service-Account-Email=email@domain"}},
	})

	writer.WriteMessage(Message{
		code:        600,
		description: "URI Acquire",
		fields:      map[string][]string{"URI": {"http://fake.uri"}, "Filename": {"/path/to/file"}},
	})

	msg, err = reader.ReadMessage(ctx)
	if err != nil {
		t.Errorf("failed, %v", err)
	}
	if msg.code != 200 || msg.description != "URI Start" ||
		msg.Get("URI") != "http://fake.uri" || msg.Get("Size") != "200" {
		t.Errorf("failed, didn't receive uri start message. msg is %q", msg)
	}

	msg, err = reader.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("failed, %v", err)
	}
	if msg.code != 201 || msg.description != "URI Done" ||
		msg.Get("URI") != "http://fake.uri" || msg.Get("Filename") != "/path/to/file" {
		t.Errorf("failed, didn't receive uri start message. msg is %q", msg)
	}
	cancel()

	// This was set after we sent the 601, but for guaranteed timing in the
	// test we don't check until after we've read a reply message.
	if workMethod.config.serviceAccountEmail != "email@domain" {
		t.Errorf("failed, didn't set method configuration. got %v, expected %q", workMethod.config.serviceAccountEmail, "email@domain")
	}

	for _, p := range []io.Closer{stdinreader, stdinwriter, stdoutreader, stdoutwriter} {
		if err := p.Close(); err != nil {
			t.Errorf("Error from %v: %v", p, err)
		}
	}
}

func TestAptMethodRun404(t *testing.T) {

	stdinreader, stdinwriter := io.Pipe()
	stdoutreader, stdoutwriter := io.Pipe()
	workMethod := NewAptMethod(bufio.NewReader(stdinreader), stdoutwriter)
	workMethod.client = fakeHTTPClient{code: 404}
	workMethod.dl = fakeDownloader{}

	ctx := context.Background()
	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()
	go workMethod.Run(ctx2)

	reader := MessageReader{reader: bufio.NewReader(stdoutreader)}
	msg, err := reader.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("failed, %v", err)
	}
	if msg.code != 100 || msg.description != "Capabilities" {
		t.Errorf("failed, didn't receive capabilities message")
	}

	writer := MessageWriter{writer: stdinwriter}
	writer.WriteMessage(Message{
		code:        600,
		description: "URI Acquire",
		fields:      map[string][]string{"URI": {"http://fake.uri"}, "Filename": {"/path/to/file"}},
	})

	msg, err = reader.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("failed, %v", err)
	}
	if msg.code != 400 || msg.description != "URI Failure" ||
		msg.Get("URI") != "http://fake.uri" || msg.Get("Message") == "" {
		t.Errorf("failed, didn't receive uri failure message. msg is %q", msg)
	}
	cancel()

	for _, p := range []io.Closer{stdinreader, stdinwriter, stdoutreader, stdoutwriter} {
		if err := p.Close(); err != nil {
			t.Errorf("Error from %v: %v", p, err)
		}
	}
}

func TestAptMethodRun304(t *testing.T) {

	stdinreader, stdinwriter := io.Pipe()
	stdoutreader, stdoutwriter := io.Pipe()
	workMethod := NewAptMethod(bufio.NewReader(stdinreader), stdoutwriter)
	workMethod.client = fakeHTTPClient{code: 304}
	workMethod.dl = fakeDownloader{}

	ctx := context.Background()
	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()
	go workMethod.Run(ctx2)

	reader := MessageReader{reader: bufio.NewReader(stdoutreader)}
	msg, err := reader.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("failed, %v", err)
	}
	if msg.code != 100 || msg.description != "Capabilities" {
		t.Errorf("failed, didn't receive capabilities message")
	}

	writer := MessageWriter{writer: stdinwriter}
	writer.WriteMessage(Message{
		code:        600,
		description: "URI Acquire",
		fields:      map[string][]string{"URI": {"http://fake.uri"}, "Filename": {"/path/to/file"}},
	})

	msg, err = reader.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("failed, %v", err)
	}
	if msg.code != 201 || msg.description != "URI Done" ||
		msg.Get("URI") != "http://fake.uri" || msg.Get("IMS-Hit") != "true" {
		t.Errorf("failed, didn't receive uri done message. msg is %q", msg)
	}
	cancel()

	for _, p := range []io.Closer{stdinreader, stdinwriter, stdoutreader, stdoutwriter} {
		if err := p.Close(); err != nil {
			t.Errorf("Error from %v: %v", p, err)
		}
	}
}

func TestAptMethodRunFail(t *testing.T) {

	stdinreader, stdinwriter := io.Pipe()
	stdoutreader, stdoutwriter := io.Pipe()
	workMethod := NewAptMethod(bufio.NewReader(stdinreader), stdoutwriter)
	workMethod.client = fakeHTTPClient{code: 404}
	workMethod.dl = fakeDownloader{}

	ctx := context.Background()
	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()
	errChan := make(chan error)
	go func() {
		errChan <- workMethod.Run(ctx2)
	}()

	reader := MessageReader{reader: bufio.NewReader(stdoutreader)}
	msg, err := reader.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("failed, %v", err)
	}
	if msg.code != 100 || msg.description != "Capabilities" {
		t.Errorf("failed, didn't receive capabilities message")
	}

	writer := MessageWriter{writer: stdinwriter}
	writer.WriteMessage(Message{
		code:        700,
		description: "Malformed message",
		fields:      map[string][]string{"": {"foo"}},
	})

	// If we receive a malformed message from `apt`, we immediately bail
	// without sending an error response. Unlike the other tests, there is no
	// protocol message to read here, but we can assert on the return value of
	// `Run`.
	runErr := <-errChan
	if runErr == nil {
		t.Fatalf("failed, expected non-nil runErr (empty key)")
	}
	if !strings.Contains(runErr.Error(), "malformed") {
		t.Fatalf("failed, expected runErr to contain 'malformed'")
	}

	for _, p := range []io.Closer{stdinreader, stdinwriter, stdoutreader, stdoutwriter} {
		if err := p.Close(); err != nil {
			t.Errorf("Error from %v: %v", p, err)
		}
	}
}
