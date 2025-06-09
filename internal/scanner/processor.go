package scanner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/artnikel/nuclei/internal/templates"
)

func matchResponse(m templates.Matcher, resp *http.Response, body []byte) bool {
	switch m.Type {
	case "status":
		for _, code := range m.Status {
			if resp.StatusCode == code {
				return true
			}
		}
	case "word":
		part := m.Part
		for _, word := range m.Words {
			if part == "body" && bytes.Contains(body, []byte(word)) {
				return true
			}
			if part == "header" {
				for k, v := range resp.Header {
					if strings.EqualFold(k, word) {
						return true
					}
					for _, hv := range v {
						if strings.Contains(hv, word) {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

func normalizeTarget(target string) string {
    if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
        return target
    }
    return "http://" + target
}


func ProcessTarget(ctx context.Context, target string, template *templates.Template, timeout time.Duration) error {
	client := &http.Client{
		Timeout: timeout,
	}

	baseURL := normalizeTarget(target)

	for _, req := range template.Requests {
		for _, path := range req.Path {
			var urlStr string
			if strings.Contains(path, "{{BaseURL}}") {
				urlStr = strings.ReplaceAll(path, "{{BaseURL}}", baseURL)
			} else {
				urlStr = baseURL + path
			}

			httpReq, err := http.NewRequestWithContext(ctx, req.Method, urlStr, nil)
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}

			for hk, hv := range req.Headers {
				httpReq.Header.Set(hk, hv)
			}

			resp, err := client.Do(httpReq)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}

			bodyBytes, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return fmt.Errorf("failed to read response body: %w", err)
			}

			if len(req.Matchers) == 0 {
				continue
			}

			matched := false
			for _, matcher := range req.Matchers {
				if matchResponse(matcher, resp, bodyBytes) {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("response for %s did not match any matcher", urlStr)
			}
		}
	}

	return nil
}
