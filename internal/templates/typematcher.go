// Package templates - matching logic for various request types
package templates

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/artnikel/nuclei/internal/constants"
	"github.com/artnikel/nuclei/internal/logging"
	"github.com/artnikel/nuclei/internal/templates/headless"
	"golang.org/x/time/rate"
)

type MatchContext struct {
	Resp     *http.Response
	Body     []byte
	DNS      *DNSResponse
	Network  *NetworkResponse
	Headless *HeadlessResponse
}

type DNSResponse struct {
	Records []string
	Raw     []byte
}

type NetworkResponse struct {
	Data []byte
}

type HeadlessResponse struct {
	RenderTime time.Duration
	HTML       string
	Screenshot []byte
	StatusCode int
	Err        error
}

var (
	hostLimitersMu sync.Mutex                       // hostLimitersMu guards access to hostLimiters map
	hostLimiters   = make(map[string]*rate.Limiter) // hostLimiters stores rate limiters per hostname

	httpClientMu sync.Mutex
	httpClient   *http.Client
)

// HTTPResult represents the result of an HTTP request
type HTTPResult struct {
	Response *http.Response
	Body     []byte
	Error    error
	Retries  int
}

func getHTTPClient(advanced *AdvancedSettingsChecker) *http.Client {
	httpClientMu.Lock()
	defer httpClientMu.Unlock()

	if httpClient == nil {
		httpClient = newInsecureHTTPClient(advanced)
	}
	return httpClient
}

// getHostLimiter returns or creates a rate limiter for a given host
func getHostLimiter(host string, advanced *AdvancedSettingsChecker) *rate.Limiter {
	hostLimitersMu.Lock()
	defer hostLimitersMu.Unlock()

	limiter, ok := hostLimiters[host]
	if !ok {
		limiter = rate.NewLimiter(rate.Every(time.Duration(advanced.RateLimiterFrequency)*time.Millisecond), advanced.RateLimiterBurstSize)
		hostLimiters[host] = limiter
	}
	return limiter
}

// isRetryableError determines if an error is worth retrying
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	retryableErrors := []string{
		"connection refused",
		"connection reset by peer",
		"no route to host",
		"network is unreachable",
		"timeout",
		"temporary failure",
		"server misbehaving",
		"connection timed out",
		"i/o timeout",
	}

	for _, retryable := range retryableErrors {
		if strings.Contains(strings.ToLower(errStr), retryable) {
			return true
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.Temporary() {
		return true
	}

	return false
}

func parseJSRedirect(body string) string {
	prefix := `top.location="`
	start := strings.Index(body, prefix)
	if start == -1 {
		return ""
	}
	start += len(prefix)
	end := strings.Index(body[start:], `"`)
	if end == -1 {
		return ""
	}
	redirectPath := body[start : start+end]
	return redirectPath
}

// doHTTPRequestWithRetry performs HTTP request with retry logic
func doHTTPRequestWithRetry(ctx context.Context, client *http.Client, req *http.Request, advanced *AdvancedSettingsChecker, logger *logging.Logger) HTTPResult {
	var lastErr error

	for attempt := 0; attempt <= advanced.Retries; attempt++ {
		reqClone := req.Clone(ctx)

		resp, err := client.Do(reqClone)
		if err == nil {
			var reader io.ReadCloser
			switch resp.Header.Get("Content-Encoding") {
			case "gzip":
				gzReader, gzErr := gzip.NewReader(resp.Body)
				if gzErr != nil {
					logger.Info.Printf("Failed to create gzip reader for %s: %v", req.URL.String(), gzErr)
					resp.Body.Close()
					return HTTPResult{Response: resp, Body: nil, Error: gzErr, Retries: attempt}
				}
				reader = gzReader
			default:
				reader = resp.Body
			}

			body, bodyErr := io.ReadAll(io.LimitReader(reader, int64(advanced.MaxBodySize)))

			if reader != resp.Body {
				reader.Close()
			}
			resp.Body.Close()

			if bodyErr != nil {
				logger.Info.Printf("Failed to read response body for %s: %v", req.URL.String(), bodyErr)
				return HTTPResult{Response: resp, Body: nil, Error: bodyErr, Retries: attempt}
			}

			return HTTPResult{Response: resp, Body: body, Error: nil, Retries: attempt}
		}

		lastErr = err

		if !isRetryableError(err) {
			break
		}

		if attempt == advanced.Retries {
			break
		}

		waitTime := advanced.RetryDelay * time.Duration(attempt+1)
		logger.Info.Printf("Request to %s failed (attempt %d/%d), retrying after %v: %v",
			req.URL.String(), attempt+1, advanced.Retries+1, waitTime, err)

		select {
		case <-ctx.Done():
			return HTTPResult{Error: ctx.Err(), Retries: attempt}
		case <-time.After(waitTime):
		}
	}

	return HTTPResult{Error: lastErr, Retries: advanced.Retries}
}



// matchHTTPRequest performs HTTP requests with improved error handling and retries
func matchHTTPRequest(ctx context.Context, baseURL string, req *Request, tmpl *Template, advanced *AdvancedSettingsChecker, logger *logging.Logger) (bool, error) {
	client := getHTTPClient(advanced)

	method := req.Method
	if method == "" {
		method = http.MethodGet
	}

	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil {
		return false, fmt.Errorf("invalid base url: %w", err)
	}
	baseURLForVars := fmt.Sprintf("%s://%s", parsedBaseURL.Scheme, parsedBaseURL.Host)

	vars := make(map[string]interface{})
	for k, v := range tmpl.Variables {
		vars[k] = v
	}
	vars["BaseURL"] = baseURLForVars
	vars["Host"] = parsedBaseURL.Host
	vars["Hostname"] = parsedBaseURL.Hostname()

	limiter := getHostLimiter(parsedBaseURL.Hostname(), advanced)

	for _, p := range req.Path {
		pathWithVars := substituteVariables(p, vars)
		currentURL := buildFullURL(parsedBaseURL, pathWithVars)

		visitedRedirects := make(map[string]struct{})
		redirectCount := 0
		maxRedirects := 5

		for {
			if redirectCount > maxRedirects {
				logger.Info.Printf("Max redirects (%d) reached for URL %s", maxRedirects, currentURL)
				break
			}
			normalizedURL := normalizeURL(currentURL)
			if _, visited := visitedRedirects[normalizedURL]; visited {
				logger.Info.Printf("Redirect loop detected at %s, stopping", currentURL)
				break
			}
			visitedRedirects[normalizedURL] = struct{}{}

			doRequest := func(url string) (HTTPResult, error) {
				httpReq, err := http.NewRequestWithContext(ctx, method, url, nil)
				if err != nil {
					return HTTPResult{}, err
				}

				httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
				httpReq.Header.Set("Accept", "*/*")
				httpReq.Header.Set("Accept-Language", "en-US,en;q=0.9")
				httpReq.Header.Set("Accept-Encoding", "gzip, deflate")
				httpReq.Header.Set("Connection", "keep-alive")

				for k, v := range req.Headers {
					httpReq.Header.Set(k, substituteVariables(v, vars))
				}

				if err := limiter.Wait(ctx); err != nil {
					return HTTPResult{}, err
				}

				return doHTTPRequestWithRetry(ctx, client, httpReq, advanced, logger), nil
			}

			result, err := doRequest(currentURL)
			if err != nil {
				logger.Error.Printf("HTTP request creation/limiter error for template %s, URL %s: %v", tmpl.ID, currentURL, err)
				break
			}

			if result.Error != nil {
				if isRetryableError(result.Error) {
					logger.Info.Printf("HTTP request failed after %d retries for template %s, URL %s: %v",
						result.Retries+1, tmpl.ID, currentURL, result.Error)
				} else {
					logger.Error.Printf("HTTP request failed for template %s, URL %s: %v",
						tmpl.ID, currentURL, result.Error)
				}
				break
			}

			if result.Retries > 0 {
				logger.Info.Printf("HTTP request succeeded after %d retries for template %s, URL %s, status %d",
					result.Retries+1, tmpl.ID, currentURL, result.Response.StatusCode)
			}

			matchCtx := MatchContext{
				Resp: result.Response,
				Body: result.Body,
			}

			matched := checkMatchers(req.Matchers, req.MatchersCondition, matchCtx, logger)
			logger.Info.Printf("Template %s, request %s: matched=%v, status=%d, retries=%d",
				tmpl.ID, currentURL, matched, result.Response.StatusCode, result.Retries)

			if matched {
				//logger.Info.Printf("Response body:\n%s", result.Body)

				// extractedData := processExtractors(req.Extractors, result, tmpl)
				// logger.Info.Printf("Extracted data: %+v", extractedData)
				return true, nil
			}

			bodyStr := string(result.Body)
			redirectPath := parseJSRedirect(bodyStr)
			if redirectPath == "" {
				break
			}

			currentURL = buildFullURL(parsedBaseURL, redirectPath)
			redirectCount++
		}
	}

	return false, nil
}

// matchDNSRequest performs DNS queries and matches the results
func matchDNSRequest(host string, req *Request, tmpl *Template, logger *logging.Logger) (bool, error) {
	queryType := "A"
	if len(req.Path) > 0 {
		queryType = strings.ToUpper(req.Path[0])
	}

	var records []string
	var err error

	switch queryType {
	case "A":
		records, err = net.LookupHost(host)
	case "AAAA":
		ips, e := net.LookupIP(host)
		if e != nil {
			err = e
		} else {
			for _, ip := range ips {
				if ip.To4() == nil {
					records = append(records, ip.String())
				}
			}
		}
	case "TXT":
		records, err = net.LookupTXT(host)
	case "CNAME":
		cname, e := net.LookupCNAME(host)
		if e != nil {
			err = e
		} else {
			records = []string{cname}
		}
	case "NS":
		nsRecords, e := net.LookupNS(host)
		if e != nil {
			err = e
		} else {
			for _, ns := range nsRecords {
				records = append(records, ns.Host)
			}
		}
	case "MX":
		mxRecords, e := net.LookupMX(host)
		if e != nil {
			err = e
		} else {
			for _, mx := range mxRecords {
				records = append(records, mx.Host)
			}
		}
	default:
		logger.Info.Printf("Unsupported DNS query type: %s\n", queryType)
		return false, nil
	}

	if err != nil {
		logger.Info.Printf("DNS lookup error for host %s: %v\n", host, err)
		return false, err
	}

	responseText := strings.Join(records, "\n")

	matchCtx := MatchContext{
		DNS: &DNSResponse{
			Records: records,
			Raw:     []byte(responseText),
		},
	}

	matched := checkMatchers(req.Matchers, req.MatchersCondition, matchCtx, logger)
	logger.Info.Printf("Template %s, DNS request for host %s, query type %s: matched=%v, records=%v",
		tmpl.ID, host, queryType, matched, records)

	return matched, nil
}

// matchNetworkRequest sends data over network connection and matches response
func matchNetworkRequest(ctx context.Context, host string, req *Request, tmpl *Template, logger *logging.Logger) (bool, error) {
	if req.Type != "network" {
		return false, fmt.Errorf("request type is not network: %s", req.Type)
	}

	protocol := "tcp"
	if protoVal, ok := req.Options["protocol"]; ok {
		if protoStr, ok := protoVal.(string); ok && protoStr != "" {
			protocol = protoStr
		}
	}

	dialer := &net.Dialer{
		Timeout: constants.TenSecTimeout,
	}
	conn, err := dialer.DialContext(ctx, protocol, host)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	var toSend []byte
	if raw, ok := req.Payloads["default"]; ok {
		switch v := raw.(type) {
		case []interface{}:
			if len(v) > 0 {
				if s, ok := v[0].(string); ok {
					toSend = []byte(s)
				}
			}
		case []string:
			if len(v) > 0 {
				toSend = []byte(v[0])
			}
		case map[string]interface{}:

			for _, inner := range v {
				if arr, ok := inner.([]interface{}); ok && len(arr) > 0 {
					if s, ok := arr[0].(string); ok {
						toSend = []byte(s)
						break
					}
				}
			}
		}
	}

	if len(toSend) > 0 {
		_, err = conn.Write(toSend)
		if err != nil {
			return false, err
		}
	}

	buf := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(constants.FiveSecTimeout))

	n, err := conn.Read(buf)
	if err != nil {
		return false, err
	}

	response := buf[:n]

	matchCtx := MatchContext{
		Network: &NetworkResponse{
			Data: response,
		},
	}

	matched := checkMatchers(req.Matchers, req.MatchersCondition, matchCtx, logger)

	logger.Info.Printf("Template %s, network request to %s: matched=%v", tmpl.ID, host, matched)

	return matched, nil
}

// matchHeadlessRequest runs headless browser requests and matches output
func matchHeadlessRequest(ctx context.Context, baseURL string, req *Request, tmpl *Template, advanced *AdvancedSettingsChecker, logger *logging.Logger) (bool, error) {
	var url string
	if len(req.Path) > 0 {
		url = baseURL + req.Path[0]
	} else {
		url = baseURL
	}

	htmlContent, err := headless.DoHeadlessRequest(ctx, url, advanced.HeadlessTabs, advanced.Timeout)
	if err != nil {
		logger.Error.Printf("Headless request failed: %v", err)
		return false, err
	}

	matchCtx := MatchContext{
		Body: []byte(htmlContent),
	}

	matched := checkMatchers(req.Matchers, req.MatchersCondition, matchCtx, logger)

	logger.Info.Printf(
		"Template %s, headless request to %s: matched=%v, response_len=%d",
		tmpl.ID, baseURL, matched, len(htmlContent),
	)

	return matched, nil
}

// matchOfflineHTML matches patterns against offline HTML content
func matchOfflineHTML(html string, req *Request, tmpl *Template, logger *logging.Logger) bool {
	for _, matcher := range req.Matchers {
		switch matcher.Type {
		case "word":
			for _, word := range matcher.Words {
				if strings.Contains(html, word) {
					logger.Info.Printf(
						"Template %s, offline matcher type=word matched word=%q matched=true", tmpl.ID, word)
					return true
				}
			}
		case "regex":
			for _, pattern := range matcher.Regex {
				re, err := regexp.Compile(pattern)
				if err != nil {
					logger.Info.Printf("Invalid regex in template %s: %v", tmpl.ID, err)
					continue
				}
				if re.MatchString(html) {
					logger.Info.Printf(
						"Template %s, offline matcher type=regex matched pattern=%q matched=true", tmpl.ID, pattern)
					return true
				}
			}
		default:
			logger.Info.Printf("Unsupported offline matcher type: %s", matcher.Type)
		}
	}
	return false
}
