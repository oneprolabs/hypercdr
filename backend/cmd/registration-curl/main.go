// registration-curl implements only the curl flags used by HyperCDR's fixed
// registration installer. It avoids a package-manager dependency in the
// hardened executor image and deliberately is not a general curl replacement.
package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var rawURL, output, writeFormat, data string
	headers, insecure, failHTTP, retries, connectTimeout := []string{}, false, false, 0, 10*time.Second
	for index := 0; index < len(args); index++ {
		arg := args[index]
		next := func() (string, error) {
			if index+1 >= len(args) {
				return "", fmt.Errorf("missing value for %s", arg)
			}
			index++
			return args[index], nil
		}
		switch arg {
		case "-k", "--insecure":
			insecure = true
		case "-f", "--fail":
			failHTTP = true
		case "-s", "-S", "-L", "-sS", "-fsSL", "-kfsSL":
			if strings.Contains(arg, "k") {
				insecure = true
			}
			if strings.Contains(arg, "f") {
				failHTTP = true
			}
		case "-o", "--output":
			value, err := next()
			if err != nil {
				return err
			}
			output = value
		case "-w", "--write-out":
			value, err := next()
			if err != nil {
				return err
			}
			writeFormat = value
		case "-H", "--header":
			value, err := next()
			if err != nil {
				return err
			}
			headers = append(headers, value)
		case "--data", "--data-raw":
			value, err := next()
			if err != nil {
				return err
			}
			data = value
		case "--retry":
			value, err := next()
			if err != nil {
				return err
			}
			retries, err = strconv.Atoi(value)
			if err != nil || retries < 0 || retries > 5 {
				return fmt.Errorf("invalid retry count")
			}
		case "--connect-timeout":
			value, err := next()
			if err != nil {
				return err
			}
			seconds, err := strconv.Atoi(value)
			if err != nil || seconds < 1 || seconds > 60 {
				return fmt.Errorf("invalid connect timeout")
			}
			connectTimeout = time.Duration(seconds) * time.Second
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("unsupported registration HTTP option: %s", arg)
			}
			if rawURL != "" {
				return fmt.Errorf("multiple URLs are not supported")
			}
			rawURL = arg
		}
	}
	if rawURL == "" {
		return fmt.Errorf("URL is required")
	}
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: connectTimeout}).DialContext, TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure}} // #nosec G402 -- explicit installer -k behavior
	client := &http.Client{Transport: transport, Timeout: 90 * time.Second}
	var response *http.Response
	var err error
	for attempt := 0; attempt <= retries; attempt++ {
		method := http.MethodGet
		var body io.Reader
		if data != "" {
			method, body = http.MethodPost, bytes.NewBufferString(data)
		}
		request, requestErr := http.NewRequest(method, rawURL, body)
		if requestErr != nil {
			return requestErr
		}
		for _, header := range headers {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid header")
			}
			request.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
		response, err = client.Do(request)
		if err == nil {
			break
		}
		if attempt < retries {
			time.Sleep(time.Duration(attempt+1) * time.Second)
		}
	}
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if failHTTP && response.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	var destination io.Writer = os.Stdout
	var file *os.File
	if output != "" {
		file, err = os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer file.Close()
		destination = file
	}
	if _, err = io.Copy(destination, io.LimitReader(response.Body, 32<<20)); err != nil {
		return err
	}
	if writeFormat != "" {
		_, err = fmt.Fprint(os.Stdout, strings.ReplaceAll(writeFormat, "%{http_code}", strconv.Itoa(response.StatusCode)))
	}
	return err
}
