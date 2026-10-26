package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"

	cycletls "github.com/talrasha007/CycleTLS"
)

type requestSpec struct {
	method          string
	url             string
	headers         http.Header
	body            string
	proxy           string
	resolveIP       string
	resolveEntries  []resolveEntry
	localIP         string
	headOnly        bool
	includeHeaders  bool
	outputPath      string
	followRedirects bool
	insecureTLS     bool
	forceTLS12      bool
	forceHTTP1      bool
	forceHTTP3      bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fpValue, curlArgs, err := extractFPArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	spec, err := parseCurlArgs(curlArgs)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	if err := executeRequest(spec, fpValue, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	return 0
}

func extractFPArgs(args []string) (*string, []string, error) {
	var fpValue *string
	passthrough := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "--fp":
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("--fp requires a value")
			}

			value := args[i+1]
			fpValue = &value
			i++
		case strings.HasPrefix(arg, "--fp="):
			value := strings.TrimPrefix(arg, "--fp=")
			fpValue = &value
		default:
			passthrough = append(passthrough, arg)
		}
	}

	return fpValue, passthrough, nil
}

func parseCurlArgs(args []string) (requestSpec, error) {
	spec := requestSpec{
		headers: make(http.Header),
	}

	var bodyParts []string

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "-X" || arg == "--request":
			value, next, err := requireNextValue(args, i, arg)
			if err != nil {
				return requestSpec{}, err
			}
			spec.method = strings.ToUpper(value)
			i = next
		case strings.HasPrefix(arg, "--request="):
			spec.method = strings.ToUpper(strings.TrimPrefix(arg, "--request="))
		case arg == "-H" || arg == "--header":
			value, next, err := requireNextValue(args, i, arg)
			if err != nil {
				return requestSpec{}, err
			}
			if err := addHeader(spec.headers, value); err != nil {
				return requestSpec{}, err
			}
			i = next
		case strings.HasPrefix(arg, "--header="):
			if err := addHeader(spec.headers, strings.TrimPrefix(arg, "--header=")); err != nil {
				return requestSpec{}, err
			}
		case arg == "-A" || arg == "--user-agent":
			value, next, err := requireNextValue(args, i, arg)
			if err != nil {
				return requestSpec{}, err
			}
			spec.headers.Set("User-Agent", value)
			i = next
		case strings.HasPrefix(arg, "--user-agent="):
			spec.headers.Set("User-Agent", strings.TrimPrefix(arg, "--user-agent="))
		case arg == "-d" || arg == "--data" || arg == "--data-raw" || arg == "--data-binary":
			value, next, err := requireNextValue(args, i, arg)
			if err != nil {
				return requestSpec{}, err
			}
			bodyParts = append(bodyParts, value)
			i = next
		case strings.HasPrefix(arg, "--data="):
			bodyParts = append(bodyParts, strings.TrimPrefix(arg, "--data="))
		case strings.HasPrefix(arg, "--data-raw="):
			bodyParts = append(bodyParts, strings.TrimPrefix(arg, "--data-raw="))
		case strings.HasPrefix(arg, "--data-binary="):
			bodyParts = append(bodyParts, strings.TrimPrefix(arg, "--data-binary="))
		case arg == "-I" || arg == "--head":
			spec.headOnly = true
		case arg == "-i" || arg == "--include":
			spec.includeHeaders = true
		case arg == "-o" || arg == "--output":
			value, next, err := requireNextValue(args, i, arg)
			if err != nil {
				return requestSpec{}, err
			}
			spec.outputPath = value
			i = next
		case strings.HasPrefix(arg, "--output="):
			spec.outputPath = strings.TrimPrefix(arg, "--output=")
		case arg == "-x" || arg == "--proxy":
			value, next, err := requireNextValue(args, i, arg)
			if err != nil {
				return requestSpec{}, err
			}
			spec.proxy = value
			i = next
		case strings.HasPrefix(arg, "--proxy="):
			spec.proxy = strings.TrimPrefix(arg, "--proxy=")
		case arg == "--resolve" || arg == "--interface" || strings.HasPrefix(arg, "--resolve=") || strings.HasPrefix(arg, "--interface="):
			flag, value, hasValue := strings.Cut(arg, "=")
			if !hasValue {
				var err error
				value, i, err = requireNextValue(args, i, flag)
				if err != nil {
					return requestSpec{}, err
				}
			}
			if flag == "--resolve" {
				if err := addResolve(&spec, value); err != nil {
					return requestSpec{}, err
				}
			} else {
				if net.ParseIP(value) == nil {
					return requestSpec{}, fmt.Errorf("invalid --interface IP: %s", value)
				}
				spec.localIP = value
			}
		case arg == "-L" || arg == "--location":
			spec.followRedirects = true
		case arg == "-k" || arg == "--insecure":
			spec.insecureTLS = true
		case arg == "--tlsv1.2":
			spec.forceTLS12 = true
		case arg == "--tls-max":
			value, next, err := requireNextValue(args, i, arg)
			if err != nil {
				return requestSpec{}, err
			}
			if value != "1.2" {
				return requestSpec{}, fmt.Errorf("unsupported --tls-max value: %s", value)
			}
			spec.forceTLS12 = true
			i = next
		case strings.HasPrefix(arg, "--tls-max="):
			value := strings.TrimPrefix(arg, "--tls-max=")
			if value != "1.2" {
				return requestSpec{}, fmt.Errorf("unsupported --tls-max value: %s", value)
			}
			spec.forceTLS12 = true
		case arg == "--http1.1":
			spec.forceHTTP1 = true
		case arg == "--http3":
			spec.forceHTTP3 = true
		case strings.HasPrefix(arg, "-"):
			return requestSpec{}, fmt.Errorf("unsupported curl flag: %s", arg)
		default:
			if spec.url != "" {
				return requestSpec{}, fmt.Errorf("multiple URLs are not supported")
			}
			spec.url = arg
		}
	}

	if spec.url == "" {
		return requestSpec{}, fmt.Errorf("missing request URL")
	}

	spec.body = strings.Join(bodyParts, "&")

	switch {
	case spec.method != "":
	case spec.headOnly:
		spec.method = http.MethodHead
	case spec.body != "":
		spec.method = http.MethodPost
	default:
		spec.method = http.MethodGet
	}

	if spec.body != "" && spec.headers.Get("Content-Type") == "" {
		spec.headers.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	return spec, nil
}

func executeRequest(spec requestSpec, fpValue *string, stdout io.Writer) error {
	client := cycletls.Init()
	defer client.Close()

	resp, err := doRequest(client, spec, fpValue)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	if spec.headOnly || spec.includeHeaders {
		if err := writeResponseHead(stdout, resp); err != nil {
			return err
		}
	}

	if spec.headOnly {
		return nil
	}

	target := stdout
	var file *os.File
	if spec.outputPath != "" {
		file, err = os.Create(spec.outputPath)
		if err != nil {
			return fmt.Errorf("open output file failed: %w", err)
		}
		defer file.Close()
		target = file
	}

	if _, err := io.WriteString(target, resp.Body); err != nil {
		return fmt.Errorf("read response failed: %w", err)
	}

	return nil
}

func buildCycleTLSOptions(spec requestSpec, fpValue *string) cycletls.Options {
	headers := make(map[string]string, len(spec.headers))
	for key, values := range spec.headers {
		headers[key] = strings.Join(values, ", ")
	}

	userAgent := headers["User-Agent"]
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/83.0.4103.106 Safari/537.36"
	}

	profile := resolveFingerprintProfile(fpValue)
	return cycletls.Options{
		Headers:                  headers,
		Body:                     spec.body,
		Proxy:                    spec.proxy,
		IP:                       spec.connectionIP(),
		LocalIP:                  spec.localIP,
		ShuffleExtensions:        profile.shuffleExtensions,
		EnableClientSessionCache: true,
		Meta:                     "ignore_ja3",
		EnableConnectionReuse:    false,
		MaxIdleClients:           128,
		MaxTotalRequests:         2,
		MaxResponseBodySize:      -1,
		SignatureAlgorithms:      profile.signatureAlgorithms,
		Ja3:                      profile.ja3,
		UserAgent:                userAgent,
		DisableRedirect:          !spec.followRedirects,
		InsecureSkipVerify:       spec.insecureTLS,
		ForceTLS12:               spec.forceTLS12 || profile.forceTLS12,
		ForceHTTP1:               spec.forceHTTP1 || profile.forceHTTP1,
		ForceHTTP3:               spec.forceHTTP3,
	}
}

type resolveEntry struct {
	host string
	port string
	ip   string
}

func addResolve(spec *requestSpec, value string) error {
	if ip := net.ParseIP(value); ip != nil {
		spec.resolveIP = ip.String()
		return nil
	}
	parts := strings.SplitN(value, ":", 3)
	if len(parts) == 3 && parts[0] != "" {
		port, err := strconv.Atoi(parts[1])
		address := parts[2]
		if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
			address = address[1 : len(address)-1]
		}
		if ip := net.ParseIP(address); err == nil && port > 0 && port <= 65535 && ip != nil {
			spec.resolveEntries = append(spec.resolveEntries, resolveEntry{
				host: strings.ToLower(parts[0]), port: strconv.Itoa(port), ip: ip.String(),
			})
			return nil
		}
	}
	return fmt.Errorf("invalid --resolve value %q: expected IP or host:port:IP", value)
}

func (spec requestSpec) connectionIP() string {
	if spec.resolveIP != "" {
		return spec.resolveIP
	}
	u, err := url.Parse(spec.url)
	if err != nil {
		return ""
	}
	port := u.Port()
	if port == "" {
		port = "443"
		if strings.EqualFold(u.Scheme, "http") {
			port = "80"
		}
	}
	wildcard := ""
	for i := len(spec.resolveEntries) - 1; i >= 0; i-- {
		entry := spec.resolveEntries[i]
		if entry.port != port {
			continue
		}
		if strings.EqualFold(entry.host, u.Hostname()) {
			return entry.ip
		}
		if entry.host == "*" && wildcard == "" {
			wildcard = entry.ip
		}
	}
	return wildcard
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func doRequest(client cycletls.CycleTLS, spec requestSpec, fpValue *string) (cycletls.Response, error) {
	if !spec.followRedirects || len(spec.resolveEntries) == 0 || spec.resolveIP != "" {
		return client.Do(spec.url, buildCycleTLSOptions(spec, fpValue), spec.method)
	}
	// Use net/http's redirect policy for method, body, cookie and sensitive-header
	// handling, but send each hop through CycleTLS with its own resolve match.
	var last cycletls.Response
	jar, _ := cookiejar.New(nil)
	redirectClient := &http.Client{Jar: jar, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		hop := spec
		hop.url, hop.method, hop.headers = req.URL.String(), req.Method, req.Header
		hop.followRedirects = false
		hop.body = ""
		if req.Body != nil {
			body, err := io.ReadAll(req.Body)
			req.Body.Close()
			if err != nil {
				return nil, err
			}
			hop.body = string(body)
		}
		resp, err := client.Do(hop.url, buildCycleTLSOptions(hop, fpValue), hop.method)
		if err != nil {
			return nil, err
		}
		last = resp
		headers := make(http.Header, len(resp.Headers))
		for key, value := range resp.Headers {
			headers.Set(key, value)
		}
		return &http.Response{StatusCode: resp.Status, Header: headers,
			Body: io.NopCloser(strings.NewReader(resp.Body)), Request: req}, nil
	})}
	req, err := http.NewRequest(spec.method, spec.url, strings.NewReader(spec.body))
	if err != nil {
		return cycletls.Response{}, err
	}
	req.Header = spec.headers.Clone()
	resp, err := redirectClient.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	return last, err
}

const (
	defaultSignatureAlgorithms   = "RAND"
	defaultJA3                   = "RAND"
	chromeSignatureAlgorithms    = "0403,0804,0401,0503,0805,0501,0806,0601"
	chromeJA3                    = "771,4865-4866-4867-49195-49199-49196-49200-52393-52392-49171-49172-156-157-47-53,35-11-65281-10-18-0-45-5-23-16-65037-51-43-27-17513-13,29-23-24-25,0"
	chrome148SignatureAlgorithms = "0403,0804,0401,0503,0805,0501,0806,0601"
	chrome148JA3                 = "771,4865-4866-4867-49195-49199-49196-49200-52393-52392-49171-49172-156-157-47-53,5-18-10-17613-65281-16-27-65037-0-43-51-11-35-13-23-45,4588-29-23-24,0"
	curlSignatureAlgorithms      = "0403,0503,0603,0807,0808,0809,080a,080b,0804,0805,0806,0401,0501,0601,0303,0301,0302,0402,0502,0602"
	curlJA3                      = "771,4866-4867-4865-49196-49200-159-52393-52392-52394-49195-49199-158-49188-49192-107-49187-49191-103-49162-49172-57-49161-49171-51-157-156-61-60-53-47-255,0-11-10-16-22-23-49-13-43-45-51-21,29-23-30-25-24-256-257-258-259-260,0-1-2"
	java21SignatureAlgorithms    = "0403,0503,0603,0807,0808,0804,0805,0806,0809,080a,080b,0401,0501,0601,0402,0303,0301,0302,0203,0201,0202"
	java21JA3                    = "771,4866-4865-4867-49196-49195-52393-49200-52392-49199-159-52394-163-158-162-49188-49192-49187-49191-107-106-103-64-49162-49172-49161-49171-57-56-51-50-157-156-61-60-53-47-255,0-5-10-11-17-23-35-13-43-45-50-51,29-23-24-25-30-256-257-258-259-260,0"
	appleTvSignatureAlgorithms   = "0403,0804,0401,0503,0805,0805,0501,0806,0601,0201"
	appleTvJA3                   = "771,4866-4867-4865-49196-49195-52393-49200-49199-52392-49162-49161-49172-49171-157-156-53-47-49160-49170-10,0-23-65281-10-11-16-5-13-18-51-45-43-27,4588-29-23-24-25,0"
	androidTVSignatureAlgorithms = "0403,0804,0401,0503,0805,0501,0806,0601,0201"
	androidTVJA3                 = "771,4865-4866-4867-49195-49196-52393-49199-49200-52392-49161-49162-49171-49172-156-157-47-53,0-23-65281-10-11-35-16-5-13-51-45-43-21,29-23-24,0"
	ctvSignatureAlgorithms       = "0403,0804,0401,0503,0805,0501,0806,0601,0201"
	ctvJa3                       = "771,49195-49196-52393-49199-49200-52392-49171-49172-156-157-47-53,65281-0-23-35-13-5-16-11-10,29-23-24,0"

	ctv1Ja3 = "771,49195-49196-52393-49199-49200-52392-49161-49162-49171-49172-156-157-47-53,65281-0-23-35-13-5-16-11-10,29-23-24,0"
	ctv2Ja3 = "771,49195-49196-52393-49199-49200-52392-49171-49172-156-157-47-53,65281-0-23-35-13-5-16-11-10-21,29-23-24,0"
)

type fingerprintProfile struct {
	shuffleExtensions   bool
	forceTLS12          bool
	forceHTTP1          bool
	signatureAlgorithms string
	ja3                 string
}

var defaultFingerprintProfile = fingerprintProfile{
	shuffleExtensions:   true,
	signatureAlgorithms: defaultSignatureAlgorithms,
	ja3:                 defaultJA3,
}

var fingerprintProfiles = map[string]fingerprintProfile{
	"chrome": {
		shuffleExtensions:   true,
		signatureAlgorithms: chromeSignatureAlgorithms,
		ja3:                 chromeJA3,
	},
	"chrome148": {
		shuffleExtensions:   true,
		forceTLS12:          true,
		signatureAlgorithms: chrome148SignatureAlgorithms,
		ja3:                 chrome148JA3,
	},
	"curl": {
		signatureAlgorithms: curlSignatureAlgorithms,
		ja3:                 curlJA3,
	},
	"curl_shuffle": {
		shuffleExtensions:   true,
		signatureAlgorithms: curlSignatureAlgorithms,
		ja3:                 curlJA3,
	},
	"java": {
		signatureAlgorithms: java21SignatureAlgorithms,
		ja3:                 java21JA3,
	},
	"apple_tv": {
		shuffleExtensions:   true,
		forceTLS12:          true,
		signatureAlgorithms: appleTvSignatureAlgorithms,
		ja3:                 appleTvJA3,
	},
	"android_tv": {
		shuffleExtensions:   true,
		signatureAlgorithms: androidTVSignatureAlgorithms,
		ja3:                 androidTVJA3,
	},
	"ctv": {
		forceTLS12:          true,
		signatureAlgorithms: ctvSignatureAlgorithms,
		ja3:                 ctvJa3,
	},
	"ctv1": {
		forceTLS12:          true,
		signatureAlgorithms: ctvSignatureAlgorithms,
		ja3:                 ctv1Ja3,
		forceHTTP1:          true,
	},
	"ctv2": {
		forceTLS12:          true,
		signatureAlgorithms: ctvSignatureAlgorithms,
		ja3:                 ctv2Ja3,
	},
}

func resolveFingerprintProfile(fpValue *string) fingerprintProfile {
	if fpValue != nil {
		if profile, ok := fingerprintProfiles[strings.ToLower(*fpValue)]; ok {
			return profile
		}
	}

	return defaultFingerprintProfile
}

func writeResponseHead(w io.Writer, resp cycletls.Response) error {
	if _, err := fmt.Fprintf(w, "HTTP/2 %d %s\r\n", resp.Status, http.StatusText(resp.Status)); err != nil {
		return fmt.Errorf("write response status failed: %w", err)
	}

	for name, value := range resp.Headers {
		if _, err := fmt.Fprintf(w, "%s: %s\r\n", name, value); err != nil {
			return fmt.Errorf("write response headers failed: %w", err)
		}
	}

	if _, err := io.WriteString(w, "\r\n"); err != nil {
		return fmt.Errorf("write header terminator failed: %w", err)
	}

	return nil
}

func addHeader(headers http.Header, raw string) error {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid header: %s", raw)
	}

	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if key == "" {
		return fmt.Errorf("invalid header: %s", raw)
	}

	headers.Add(key, value)
	return nil
}

func requireNextValue(args []string, index int, flag string) (string, int, error) {
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("%s requires a value", flag)
	}

	return args[index+1], index + 1, nil
}
