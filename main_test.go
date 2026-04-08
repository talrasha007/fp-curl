package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExtractFPArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantFP   *string
		wantArgs []string
		wantErr  bool
	}{
		{
			name:     "without fp",
			args:     []string{"-X", "POST", "https://example.com"},
			wantArgs: []string{"-X", "POST", "https://example.com"},
		},
		{
			name:     "fp with separate value",
			args:     []string{"--fp", "hello", "-I", "https://example.com"},
			wantFP:   ptr("hello"),
			wantArgs: []string{"-I", "https://example.com"},
		},
		{
			name:     "fp with equal value",
			args:     []string{"--fp=hello", "-I", "https://example.com"},
			wantFP:   ptr("hello"),
			wantArgs: []string{"-I", "https://example.com"},
		},
		{
			name:    "fp missing value",
			args:    []string{"--fp"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotFP, gotArgs, err := extractFPArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("extractFPArgs() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			if !reflect.DeepEqual(gotFP, tt.wantFP) {
				t.Fatalf("extractFPArgs() fp = %v, want %v", gotFP, tt.wantFP)
			}

			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Fatalf("extractFPArgs() args = %v, want %v", gotArgs, tt.wantArgs)
			}
		})
	}
}

func TestParseCurlArgs(t *testing.T) {
	t.Parallel()

	spec, err := parseCurlArgs([]string{
		"-X", "PUT",
		"-H", "X-Test: yes",
		"--data", "a=1",
		"--data", "b=2",
		"-x", "http://127.0.0.1:8080",
		"-L",
		"-k",
		"-i",
		"https://example.com",
	})
	if err != nil {
		t.Fatalf("parseCurlArgs() error = %v", err)
	}

	if spec.method != http.MethodPut {
		t.Fatalf("parseCurlArgs() method = %s, want %s", spec.method, http.MethodPut)
	}

	if spec.url != "https://example.com" {
		t.Fatalf("parseCurlArgs() url = %s", spec.url)
	}

	if got := spec.headers.Get("X-Test"); got != "yes" {
		t.Fatalf("parseCurlArgs() header = %s, want yes", got)
	}

	if got := spec.headers.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Fatalf("parseCurlArgs() content type = %s", got)
	}

	if spec.body != "a=1&b=2" {
		t.Fatalf("parseCurlArgs() body = %s", spec.body)
	}

	if spec.proxy != "http://127.0.0.1:8080" {
		t.Fatalf("parseCurlArgs() proxy = %s", spec.proxy)
	}

	if !spec.followRedirects || !spec.insecureTLS || !spec.includeHeaders {
		t.Fatalf("parseCurlArgs() flags not applied: %+v", spec)
	}
}

func TestRunSendsHTTPRequest(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotHeader string
	var gotBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Test")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		gotBody = string(body)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{
		"--fp", "demo",
		"-X", "POST",
		"-H", "X-Test: yes",
		"-d", "name=codex",
		server.URL,
	}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("run() exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("request method = %s, want POST", gotMethod)
	}

	if gotHeader != "yes" {
		t.Fatalf("request header = %s, want yes", gotHeader)
	}

	if gotBody != "name=codex" {
		t.Fatalf("request body = %s, want name=codex", gotBody)
	}

	if stdout.String() != "demo\nok" {
		t.Fatalf("stdout = %q, want %q", stdout.String(), "demo\nok")
	}
}

func TestParseCurlArgsUserAgentHeader(t *testing.T) {
	t.Parallel()

	spec, err := parseCurlArgs([]string{
		"-H", "User-Agent: custom-agent",
		"https://example.com",
	})
	if err != nil {
		t.Fatalf("parseCurlArgs() error = %v", err)
	}

	options := buildCycleTLSOptions(spec, nil)
	if options.UserAgent != "custom-agent" {
		t.Fatalf("buildCycleTLSOptions() user agent = %s, want custom-agent", options.UserAgent)
	}
}

func TestParseCurlArgsProxyEquals(t *testing.T) {
	t.Parallel()

	spec, err := parseCurlArgs([]string{
		"--proxy=http://127.0.0.1:8888",
		"https://example.com",
	})
	if err != nil {
		t.Fatalf("parseCurlArgs() error = %v", err)
	}

	options := buildCycleTLSOptions(spec, nil)
	if options.Proxy != "http://127.0.0.1:8888" {
		t.Fatalf("buildCycleTLSOptions() proxy = %s, want http://127.0.0.1:8888", options.Proxy)
	}
}

func TestBuildCycleTLSOptionsDefaultFingerprint(t *testing.T) {
	t.Parallel()

	spec, err := parseCurlArgs([]string{"https://example.com"})
	if err != nil {
		t.Fatalf("parseCurlArgs() error = %v", err)
	}

	options := buildCycleTLSOptions(spec, nil)
	if !options.ShuffleExtensions {
		t.Fatalf("buildCycleTLSOptions() ShuffleExtensions = false, want true")
	}

	if options.SignatureAlgorithms != "RAND" {
		t.Fatalf("buildCycleTLSOptions() SignatureAlgorithms = %s, want RAND", options.SignatureAlgorithms)
	}

	if options.Ja3 != "RAND" {
		t.Fatalf("buildCycleTLSOptions() Ja3 = %s, want RAND", options.Ja3)
	}
}

func TestBuildCycleTLSOptionsChromeFingerprint(t *testing.T) {
	t.Parallel()

	spec, err := parseCurlArgs([]string{"https://example.com"})
	if err != nil {
		t.Fatalf("parseCurlArgs() error = %v", err)
	}

	fpValue := "ChRoMe"
	options := buildCycleTLSOptions(spec, &fpValue)

	if !options.ShuffleExtensions {
		t.Fatalf("buildCycleTLSOptions() ShuffleExtensions = false, want true")
	}

	if options.SignatureAlgorithms != "0403,0804,0401,0503,0805,0501,0806,0601" {
		t.Fatalf("buildCycleTLSOptions() SignatureAlgorithms = %s", options.SignatureAlgorithms)
	}

	if options.Ja3 != "771,4865-4866-4867-49195-49199-49196-49200-52393-52392-49171-49172-156-157-47-53,35-11-65281-10-18-0-45-5-23-16-65037-51-43-27-17513-13,29-23-24-25,0" {
		t.Fatalf("buildCycleTLSOptions() Ja3 = %s", options.Ja3)
	}
}

func TestRunWritesBodyToFile(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "saved-body")
	}))
	defer server.Close()

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "response.txt")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{
		"-o", outputPath,
		server.URL,
	}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("run() exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if string(content) != "saved-body" {
		t.Fatalf("output file = %q, want %q", string(content), "saved-body")
	}

	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunRejectsUnsupportedFlag(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"--compressed", "https://example.com"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("run() exitCode = %d, want 2", exitCode)
	}

	if !strings.Contains(stderr.String(), "unsupported curl flag") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func ptr(s string) *string {
	return &s
}
