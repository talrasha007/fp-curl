package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	cycletls "github.com/talrasha007/CycleTLS"
)

type requestSpec struct {
	method          string
	url             string
	headers         http.Header
	body            string
	proxy           string
	headOnly        bool
	includeHeaders  bool
	outputPath      string
	followRedirects bool
	insecureTLS     bool
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
		case arg == "-L" || arg == "--location":
			spec.followRedirects = true
		case arg == "-k" || arg == "--insecure":
			spec.insecureTLS = true
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

	resp, err := client.Do(spec.url, buildCycleTLSOptions(spec, fpValue), spec.method)
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

	shuffleExtensions, signatureAlgorithms, ja3 := resolveFingerprintProfile(fpValue)

	return cycletls.Options{
		Headers:                  headers,
		Body:                     spec.body,
		Proxy:                    spec.proxy,
		ShuffleExtensions:        shuffleExtensions,
		EnableClientSessionCache: true,
		Meta:                     "ignore_ja3",
		EnableConnectionReuse:    false,
		MaxIdleClients:           128,
		MaxTotalRequests:         2,
		MaxResponseBodySize:      -1,
		SignatureAlgorithms:      signatureAlgorithms,
		Ja3:                      ja3,
		UserAgent:                userAgent,
		DisableRedirect:          !spec.followRedirects,
		InsecureSkipVerify:       spec.insecureTLS,
		ForceHTTP1:               false,
		ForceHTTP3:               false,
	}
}

func resolveFingerprintProfile(fpValue *string) (bool, string, string) {
	const (
		defaultSignatureAlgorithms = "RAND"
		defaultJA3                 = "RAND"
		chromeSignatureAlgorithms  = "0403,0804,0401,0503,0805,0501,0806,0601"
		chromeJA3                  = "771,4865-4866-4867-49195-49199-49196-49200-52393-52392-49171-49172-156-157-47-53,35-11-65281-10-18-0-45-5-23-16-65037-51-43-27-17513-13,29-23-24-25,0"
		curlSignatureAlgorithms    = "0403,0503,0603,0807,0808,0809,080a,080b,0804,0805,0806,0401,0501,0601,0303,0301,0302,0402,0502,0602"
		curlJA3                    = "771,4866-4867-4865-49196-49200-159-52393-52392-52394-49195-49199-158-49188-49192-107-49187-49191-103-49162-49172-57-49161-49171-51-157-156-61-60-53-47-255,0-11-10-16-22-23-49-13-43-45-51-21,29-23-30-25-24-256-257-258-259-260,0-1-2"
	)

	if fpValue != nil && strings.EqualFold(*fpValue, "chrome") {
		return true, chromeSignatureAlgorithms, chromeJA3
	}

	if fpValue != nil && strings.EqualFold(*fpValue, "curl") {
		return false, curlSignatureAlgorithms, curlJA3
	}

	return true, defaultSignatureAlgorithms, defaultJA3
}

func writeResponseHead(w io.Writer, resp cycletls.Response) error {
	if _, err := fmt.Fprintf(w, "HTTP/1.1 %d %s\r\n", resp.Status, http.StatusText(resp.Status)); err != nil {
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
