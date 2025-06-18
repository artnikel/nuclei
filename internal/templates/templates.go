// package templates represents the processing of templates
package templates

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"slices"

	"github.com/artnikel/nuclei/internal/constants"
	"github.com/artnikel/nuclei/internal/logging"
	"github.com/artnikel/nuclei/internal/templates/headless"
	"gopkg.in/yaml.v3"
)

type AdvancedSettingsChecker struct {
	Workers              int
	Timeout              time.Duration
	Retries              int
	RetryDelay           time.Duration
	MaxBodySize          int
	ConnectionTimeout    time.Duration
	ReadTimeout          time.Duration
	HeadlessTabs         int
	RateLimiterFrequency int
	RateLimiterBurstSize int
}

// LoadTemplate loads and parses YAML template from the specified path
func LoadTemplate(path string) (*Template, error) {
	if !(strings.HasSuffix(path, constants.YamlFileFormat) || strings.HasSuffix(path, constants.YmlFileFormat)) {
		return nil, fmt.Errorf("file is not a YAML template: %s", path)
	}

	bs, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	tmpl := &Template{}
	if err := yaml.Unmarshal(bs, tmpl); err != nil {
		isProfile, _ := isProfileFile(bs)
		if isProfile {
			return nil, fmt.Errorf("skipping profile file: %s", path)
		}
		return nil, fmt.Errorf("failed to parse file %s: %w", path, err)
	}
	tmpl.NormalizeRequests()

	tmpl.Requests = append(tmpl.Requests, tmpl.RequestsRaw...)
	tmpl.Requests = append(tmpl.Requests, tmpl.HTTPRaw...)

	return tmpl, nil
}

// LoadTemplates loads and parses YAML templates from the specified directory
func LoadTemplates(dir string, logger *logging.Logger) ([]*Template, error) {
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
		tmpl, err := LoadTemplate(path)
		if err != nil {
			logger.Info.Printf("skipping file %s: %v", path, err)
			return nil
		}

		templates = append(templates, tmpl)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return templates, nil
}

// FindMatchingTemplates searches for matching templates for the specified URL, executing them in parallel
func FindMatchingTemplates(ctx context.Context,
	targetURL string,
	templatesDir string,
	timeout time.Duration,
	advanced *AdvancedSettingsChecker,
	logger *logging.Logger,
	progressCallback func(i, total int)) ([]*Template, error) {

	templates, err := LoadTemplates(templatesDir, logger)
	if err != nil {
		return nil, err
	}

	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}
	targetHost := parsedURL.Hostname()

	htmlContent, err := headless.DoHeadlessRequest(ctx, targetURL, advanced.HeadlessTabs, advanced.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch HTML for %s: %w", targetURL, err)
	}

	var matchedTemplates []*Template
	var mu sync.Mutex
	var wg sync.WaitGroup

	total := len(templates)

	var counter atomic.Int32

	sem := make(chan struct{}, advanced.Workers)

	for _, tmpl := range templates {
		select {
		case <-ctx.Done():
			return matchedTemplates, ctx.Err()
		default:
		}
		if !templateMatchesHost(tmpl, targetHost, logger) {
			current := int(counter.Add(1))
			progressCallback(current, total)
			continue
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(t *Template) {
			defer wg.Done()
			defer func() { <-sem }()

			select {
			case <-ctx.Done():
				return
			default:
			}

			matches, err := MatchTemplate(ctx, targetURL, htmlContent, t, advanced, logger)
			if err == nil && matches {
				mu.Lock()
				matchedTemplates = append(matchedTemplates, t)
				mu.Unlock()
			}
			current := int(counter.Add(1))
			progressCallback(current, total)
		}(tmpl)
	}

	wg.Wait()
	templates = nil
	runtime.GC()
	return matchedTemplates, nil
}

// MatchTemplate executes HTTP requests from the template and checks if the response matches the matchers conditions
func MatchTemplate(ctx context.Context, baseURL string, htmlContent string, tmpl *Template, advanced *AdvancedSettingsChecker, logger *logging.Logger) (bool, error) {
	if len(tmpl.Requests) == 0 {
		return false, fmt.Errorf("template %s has no requests", tmpl.ID)
	}

	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return false, err
	}
	host := parsedURL.Hostname()

	for _, req := range tmpl.Requests {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}

		var matched bool
		var err error

		switch req.Type {
		case "http", "":
			canOffline := canOfflineMatchRequest(req)

			if canOffline && htmlContent != "" {
				if matchOfflineHTML(htmlContent, req, tmpl, logger) {
					return true, nil
				}
			}

			matched, err := matchHTTPRequest(ctx, baseURL, req, tmpl, advanced, logger)
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}

		case "dns", "CNAME", "NS", "TXT", "A", "CAA", "DS", "AAAA", "MX", "PTR", "SOA":
			matched, err = matchDNSRequest(host, req, tmpl, logger)
		case "network":
			matched, err = matchNetworkRequest(ctx, host, req, tmpl, logger)
		case "headless":
			if canOfflineMatchRequest(req) {
				matched := matchOfflineHTML(htmlContent, req, tmpl, logger)
				if matched {
					return true, nil
				}
			} else {
				matched, err := matchHeadlessRequest(ctx, baseURL, req, tmpl, advanced, logger)
				if err != nil {
					return false, err
				}
				if matched {
					return true, nil
				}
			}
		default:
			logger.Info.Printf("Unsupported request type: %s\n", req.Type)
			continue
		}

		if err != nil {
			logger.Info.Printf("Request failed: %v", err)
			continue
		}

		if matched {
			return true, nil
		}
	}

	return false, nil
}

// checkMatchers checks the list of matchers according to the given condition (and/or)
func checkMatchers(matchers []Matcher, condition string, ctx MatchContext) bool {
	if len(matchers) == 0 {
		return true
	}

	condition = strings.ToLower(condition)
	if condition == "" {
		condition = "and"
	}

	results := make([]bool, len(matchers))
	for i, m := range matchers {
		results[i] = checkSingleMatcher(m, ctx)
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
func checkSingleMatcher(m Matcher, ctx MatchContext) bool {
	switch m.Type {
	case "status":
		if ctx.Resp == nil {
			return false
		}
		return slices.Contains(m.Status, ctx.Resp.StatusCode)

	case "word":
		if ctx.Resp == nil {
			return false
		}
		return matchWordsByPart(ctx.Resp, ctx.Body, m.Words, m.Part, m.Condition, m.NoCase)

	case "regex":
		if ctx.Resp == nil {
			return false
		}
		return matchRegexListByPart(ctx.Resp, ctx.Body, m.Regex, m.Part, m.NoCase)

	case "size":
		if ctx.Resp == nil {
			return false
		}
		return matchSizeByPart(ctx.Resp, ctx.Body, m.Size, m.Part)

	case "dlength":
		if ctx.Resp == nil {
			return false
		}
		return matchDlengthByPart(ctx.Resp, ctx.Body, m.Condition, m.Dlength, m.Part)

	case "binary":
		if ctx.Resp == nil {
			return false
		}
		var binaries [][]byte
		for _, b := range m.Binary {
			binaries = append(binaries, []byte(b))
		}
		return matchBinaryByPart(ctx.Resp, ctx.Body, binaries, m.Part)
	case "xpath":
		if ctx.Body == nil {
			return false
		}
		for _, xpath := range m.XPath {
			if matchXPathByPart(ctx.Body, xpath) {
				return true
			}
		}
		return false

	case "json":
		if ctx.Body == nil {
			return false
		}
		return matchJSONByPart(ctx.Body, m.JSONPath)

	case "dns":
		if ctx.DNS == nil {
			return false
		}
		return matchDNSByPattern(ctx.DNS, m.Pattern)
	case "network":
		if ctx.Network == nil {
			return false
		}
		return matchNetworkByPattern(ctx.Network, m.Pattern)
	case "headless":
		if ctx.Headless == nil {
			return false
		}
		return matchHeadlessByPattern(ctx.Headless, m)
	default:
		return false
	}
}
