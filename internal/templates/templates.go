// package templates represents the processing of templates
package templates

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"slices"

	"github.com/artnikel/nuclei/internal/constants"
	"github.com/artnikel/nuclei/internal/logging"
	"gopkg.in/yaml.v3"
)

// LoadTemplates loads and parses YAML templates from the specified directory
func LoadTemplates(dir string) ([]*Template, error) {
	var templates []*Template
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !(strings.HasSuffix(d.Name(), constants.YamlFileFormat) || strings.HasSuffix(d.Name(), constants.YmlFileFormat)) {
			return nil
		}
		bs, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		tmpl := &Template{}
		if err := yaml.Unmarshal(bs, tmpl); err != nil {
			return fmt.Errorf("failed to parse template %s: %w", path, err)
		}
		tmpl.Requests = append(tmpl.Requests, tmpl.RequestsRaw...)
		tmpl.Requests = append(tmpl.Requests, tmpl.HTTPRaw...)

		templates = append(templates, tmpl)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return templates, nil
}

// FindMatchingTemplates searches for matching templates for the specified URL, executing them in parallel
func FindMatchingTemplates(ctx context.Context, targetURL string, templatesDir string, timeout time.Duration, logger *logging.Logger) ([]*Template, error) {
	templates, err := LoadTemplates(templatesDir)
	if err != nil {
		return nil, err
	}

	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}
	targetHost := parsedURL.Hostname()

	var matchedTemplates []*Template

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, tmpl := range templates {
		if !templateMatchesHost(tmpl, targetHost) {
			continue
		}

		wg.Add(1)
		go func(t *Template) {
			defer wg.Done()

			matches, err := MatchTemplate(ctx, targetURL, t, logger)
			if err == nil && matches {
				mu.Lock()
				matchedTemplates = append(matchedTemplates, t)
				mu.Unlock()
			}
		}(tmpl)
	}

	wg.Wait()
	return matchedTemplates, nil
}

// MatchTemplate executes HTTP requests from the template and checks if the response matches the matchers conditions
func MatchTemplate(ctx context.Context, baseURL string, tmpl *Template, logger *logging.Logger) (bool, error) {
	client := newInsecureHTTPClient(constants.TenSecTimeout)
	if len(tmpl.Requests) == 0 {
		return false, fmt.Errorf("template %s has no requests", tmpl.ID)
	}

	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil {
		return false, fmt.Errorf("invalid target url: %w", err)
	}

	baseURLForVars := fmt.Sprintf("%s://%s", parsedBaseURL.Scheme, parsedBaseURL.Host)
	if tmpl.Variables == nil {
		tmpl.Variables = make(map[string]string)
	}
	tmpl.Variables["BaseURL"] = baseURLForVars

	for _, req := range tmpl.Requests {
		method := req.Method
		if method == "" {
			method = http.MethodGet
		}

		for _, p := range req.Path {
			pathWithVars := substituteVariables(p, tmpl.Variables)
			fullURL := buildFullURL(parsedBaseURL, pathWithVars)

			httpReq, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
			if err != nil {
				continue
			}

			for k, v := range req.Headers {
				httpReq.Header.Set(k, substituteVariables(v, tmpl.Variables))
			}

			resp, err := client.Do(httpReq)
			if err != nil {
				logger.Info.Printf("Request error for %s: %v\n", fullURL, err)
				continue
			}

			bodyBytes, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				logger.Info.Printf("Read body error for %s: %v\n", fullURL, err)
				continue
			}

			matched := checkMatchers(req.Matchers, req.MatchersCondition, resp, bodyBytes)
			logger.Info.Printf("Template %s, request %s: matched=%v, status=%d\n", tmpl.ID, fullURL, matched, resp.StatusCode)
			if matched {
				return true, nil
			}
		}
	}

	return false, nil
}

// checkMatchers checks the list of matchers according to the given condition (and/or)
func checkMatchers(matchers []Matcher, condition string, resp *http.Response, body []byte) bool {
	if len(matchers) == 0 {
		return true
	}

	condition = strings.ToLower(condition)
	if condition == "" {
		condition = "and"
	}

	results := make([]bool, len(matchers))
	for i, m := range matchers {
		results[i] = checkSingleMatcher(m, resp, body)
	}

	if condition == "or" {
		for _, r := range results {
			if r {
				return true
			}
		}
		return false
	}

	for _, r := range results {
		if !r {
			return false
		}
	}
	return true
}

// checkSingleMatcher checks a single matcher against the server response
func checkSingleMatcher(m Matcher, resp *http.Response, body []byte) bool {
	switch m.Type {
	case "status":
		return slices.Contains(m.Status, resp.StatusCode)
	case "word":
		return matchWordsByPart(resp, body, m.Words, m.Part, m.Condition, m.NoCase)
	case "regex":
		return matchRegexByPart(resp, body, m.Regex, m.Part, m.NoCase)
	case "size":
		return matchSizeByPart(resp, body, m.Size, m.Part)
	case "dlength":
		return matchDlengthByPart(resp, body, m.Condition, m.Dlength, m.Part)
	case "binary":
		return matchBinaryByPart(resp, body, []byte(m.Binary), m.Part)
	case "xpath":
		return matchXPathByPart(body, m.XPath)
	case "json":
		return matchJSONByPart(body, m.JSONPath)
	default:
		return false
	}
}
