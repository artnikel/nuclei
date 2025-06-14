package templates

import (
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
	hostLimitersMu sync.Mutex
	hostLimiters   = make(map[string]*rate.Limiter)
	rateLimit      = rate.Every(10 * time.Millisecond)
	burstLimit     = 100
)

func getHostLimiter(host string) *rate.Limiter {
	hostLimitersMu.Lock()
	defer hostLimitersMu.Unlock()

	limiter, ok := hostLimiters[host]
	if !ok {
		limiter = rate.NewLimiter(rateLimit, burstLimit)
		hostLimiters[host] = limiter
	}
	return limiter
}

func matchHTTPRequest(ctx context.Context, baseURL string, req *Request, tmpl *Template, logger *logging.Logger) (bool, error) {
	client := newInsecureHTTPClient(constants.TenSecTimeout)

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

	for _, p := range req.Path {
		pathWithVars := substituteVariables(p, vars)
		fullURL := buildFullURL(parsedBaseURL, pathWithVars)

		httpReq, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
		if err != nil {
			return false, err
		}

		for k, v := range req.Headers {
			httpReq.Header.Set(k, substituteVariables(v, vars))
		}

		limiter := getHostLimiter(parsedBaseURL.Hostname())
		for {
			err := limiter.Wait(ctx)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					logger.Info.Printf("Rate limiter wait error for host %s: %v", parsedBaseURL.Host, err)

					return false, nil
				}
				return false, err
			}
			break
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			logger.Info.Printf("HTTP request error for %s: %v", fullURL, err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			logger.Info.Printf("Failed to read body for %s: %v", fullURL, err)
			continue
		}

		matchCtx := MatchContext{
			Resp: resp,
			Body: body,
		}

		matched := checkMatchers(req.Matchers, req.MatchersCondition, matchCtx)

		logger.Info.Printf("Template %s, request %s: matched=%v, status=%d", tmpl.ID, fullURL, matched, resp.StatusCode)
		if matched {
			return true, nil
		}
	}

	return false, nil
}

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

	matched := checkMatchers(req.Matchers, req.MatchersCondition, matchCtx)
	logger.Info.Printf("Template %s, DNS request for host %s, query type %s: matched=%v, records=%v",
		tmpl.ID, host, queryType, matched, records)

	return matched, nil
}

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

	dialer := &net.Dialer{}
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
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

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

	matched := checkMatchers(req.Matchers, req.MatchersCondition, matchCtx)

	logger.Info.Printf("Template %s, network request to %s: matched=%v", tmpl.ID, host, matched)

	return matched, nil
}

func matchHeadlessRequest(ctx context.Context, baseURL string, req *Request, tmpl *Template, logger *logging.Logger) (bool, error) {
	var url string
	if len(req.Path) > 0 {
		url = baseURL + req.Path[0]
	} else {
		url = baseURL
	}

	htmlContent, err := headless.DoHeadlessRequest(ctx, url)
	if err != nil {
		logger.Error.Printf("Headless request failed: %v", err)
		return false, err
	}

	matchCtx := MatchContext{
		Body: []byte(htmlContent),
	}

	matched := checkMatchers(req.Matchers, req.MatchersCondition, matchCtx)

	logger.Info.Printf(
		"Template %s, headless request to %s: matched=%v, response_len=%d",
		tmpl.ID, baseURL, matched, len(htmlContent),
	)

	return matched, nil
}

func matchOfflineHTML(html string, req *Request, tmpl *Template, logger *logging.Logger) bool {
	for _, matcher := range req.Matchers {
		switch matcher.Type {
		case "word":
			for _, word := range matcher.Words {
				if strings.Contains(html, word) {
					logger.Info.Printf(
						"Template %s, offline matcher type=word matched word=%q", tmpl.ID, word)
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
						"Template %s, offline matcher type=regex matched pattern=%q", tmpl.ID, pattern)
					return true
				}
			}
		default:
			logger.Info.Printf("Unsupported offline matcher type: %s", matcher.Type)
		}
	}
	return false
}
