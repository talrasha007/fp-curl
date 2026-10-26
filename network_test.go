package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNetworkOptions(t *testing.T) {
	for _, tt := range []struct {
		name  string
		args  []string
		ip    string
		local string
	}{
		{"bare IP", []string{"--resolve", "192.0.2.1"}, "192.0.2.1", ""},
		{"bare IPv6", []string{"--resolve=2001:db8::1"}, "2001:db8::1", ""},
		{"curl entry", []string{"--resolve", "example.com:443:192.0.2.2"}, "192.0.2.2", ""},
		{"curl IPv6", []string{"--resolve=example.com:443:[2001:db8::2]"}, "2001:db8::2", ""},
		{"wrong host", []string{"--resolve=other.example:443:192.0.2.2"}, "", ""},
		{"wrong port", []string{"--resolve=example.com:80:192.0.2.2"}, "", ""},
		{"wildcard", []string{"--resolve=*:443:192.0.2.3"}, "192.0.2.3", ""},
		{"specific beats wildcard", []string{"--resolve=example.com:443:192.0.2.2", "--resolve=*:443:192.0.2.3"}, "192.0.2.2", ""},
		{"last entry wins", []string{"--resolve=example.com:443:192.0.2.1", "--resolve=example.com:443:192.0.2.2"}, "192.0.2.2", ""},
		{"local IP", []string{"--interface", "127.0.0.1"}, "", "127.0.0.1"},
		{"local IPv6", []string{"--interface=::1"}, "", "::1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := parseCurlArgs(append(tt.args, "https://example.com"))
			if err != nil {
				t.Fatal(err)
			}
			opts := buildCycleTLSOptions(spec, nil)
			if opts.IP != tt.ip || opts.LocalIP != tt.local {
				t.Fatalf("IP=%q LocalIP=%q, want %q %q", opts.IP, opts.LocalIP, tt.ip, tt.local)
			}
		})
	}
}

func TestInvalidNetworkOptions(t *testing.T) {
	for _, args := range [][]string{
		{"--resolve"}, {"--interface"}, {"--resolve="}, {"--interface="},
		{"--resolve=bad"}, {"--resolve=example.com:443:bad"},
		{"--resolve=example.com:0:127.0.0.1"}, {"--resolve=example.com:65536:127.0.0.1"},
		{"--resolve=:443:127.0.0.1"}, {"--interface=eth0"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(append([]string{"https://example.com"}, args...), &stdout, &stderr); code != 2 {
			t.Errorf("args %v: code=%d stderr=%s", args, code, &stderr)
		}
	}
}

func TestResolveConnectsWithOriginalHostAndLocalIP(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		io.WriteString(w, r.Host+"|"+r.TLS.ServerName+"|"+ip)
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	for _, resolve := range []string{"127.0.0.1", "example.invalid:" + port + ":127.0.0.1"} {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--resolve", resolve, "--interface=127.0.0.1", "-k", "--http1.1", "https://example.invalid:" + port}, &stdout, &stderr)
		if code != 0 || stdout.String() != "example.invalid:"+port+"|example.invalid|127.0.0.1" {
			t.Fatalf("resolve %s: code=%d stdout=%q stderr=%s", resolve, code, &stdout, &stderr)
		}
	}
}

func TestResolveRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			_, port, _ := net.SplitHostPort(r.Host)
			http.Redirect(w, r, "http://second.invalid:"+port+"/end", http.StatusFound)
			return
		}
		io.WriteString(w, r.Host)
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	for _, resolves := range [][]string{
		{"--resolve=127.0.0.1"},
		{"--resolve=first.invalid:" + port + ":127.0.0.1", "--resolve=second.invalid:" + port + ":127.0.0.1"},
		{"--resolve=second.invalid:" + port + ":127.0.0.1"},
	} {
		var stdout, stderr bytes.Buffer
		startURL := "http://first.invalid:" + port + "/start"
		if len(resolves) == 1 && strings.Contains(resolves[0], "second.invalid") {
			startURL = server.URL + "/start"
		}
		args := append(resolves, "-L", startURL)
		if code := run(args, &stdout, &stderr); code != 0 || !strings.HasPrefix(stdout.String(), "second.invalid:") {
			t.Fatalf("code=%d stdout=%q stderr=%s", code, &stdout, &stderr)
		}
	}
}
