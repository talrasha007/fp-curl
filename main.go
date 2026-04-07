package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type requestSpec struct {
	method          string
	url             string
	headers         http.Header
	body            string
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

	if fpValue != nil {
		fmt.Fprintln(stdout, *fpValue)
	}

	spec, err := parseCurlArgs(curlArgs)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	if err := executeRequest(spec, stdout); err != nil {
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

func executeRequest(spec requestSpec, stdout io.Writer) error {
	var bodyReader io.Reader
	if spec.body != "" {
		bodyReader = strings.NewReader(spec.body)
	}

	req, err := http.NewRequest(spec.method, spec.url, bodyReader)
	if err != nil {
		return fmt.Errorf("build request failed: %w", err)
	}

	req.Header = spec.headers.Clone()

	client := &http.Client{
		Transport: buildTransport(spec.insecureTLS),
	}
	if !spec.followRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

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

	if _, err := io.Copy(target, resp.Body); err != nil {
		return fmt.Errorf("read response failed: %w", err)
	}

	return nil
}

func buildTransport(insecure bool) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return transport
}

func writeResponseHead(w io.Writer, resp *http.Response) error {
	if _, err := fmt.Fprintf(w, "%s %s\r\n", resp.Proto, resp.Status); err != nil {
		return fmt.Errorf("write response status failed: %w", err)
	}

	if err := resp.Header.Write(w); err != nil {
		return fmt.Errorf("write response headers failed: %w", err)
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
