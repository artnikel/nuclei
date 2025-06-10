package scanner

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"
	"time"

	"slices"

	"github.com/artnikel/nuclei/internal/templates"
)

func matchResponse(m templates.Matcher, resp *http.Response, body []byte) bool {
	switch m.Type {
	case "status":
		if slices.Contains(m.Status, resp.StatusCode) {
			return true
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

func renderTemplateString(tmplStr string, data map[string]string) (string, error) {
	tmpl, err := template.New("tmpl").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	return buf.String(), err
}

func renderPath(baseURL, pathTmpl string) string {
    vars := map[string]string{
        "BaseURL": baseURL,
    }
    res, err := renderTemplateString(pathTmpl, vars)
    if err != nil {
        fmt.Printf("failed to render path template: %v\n", err)
        return pathTmpl
    }
    if strings.Contains(res, "{{BaseURL}}") {
        return baseURL
    }

    if !strings.HasPrefix(res, "http://") && !strings.HasPrefix(res, "https://") {
        if !strings.HasPrefix(res, "/") {
            res = "/" + res
        }
        res = baseURL + res
    }
    return res
}

func ProcessTarget(ctx context.Context, target string, template *templates.Template, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	baseURL := normalizeTarget(target)

	for _, req := range template.Requests {
		for _, pathTmpl := range req.Path {
			urlStr := renderPath(baseURL, pathTmpl)
			fmt.Printf("Resolved URL: %s\n", urlStr)

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

			fmt.Printf("Template %s, request %s: matched=%v, status=%d\n",
				template.ID, urlStr, matched, resp.StatusCode)

			if !matched {
				return fmt.Errorf("response for %s did not match any matcher", urlStr)
			}
		}
	}
	return nil
}
