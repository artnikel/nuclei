// package templates represents the processing of templates
package templates

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
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
	tmpl.FilePath = path
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

	results := make(map[int]bool)
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return false, err
	}
	host := parsedURL.Hostname()
    if tmpl.Flow != "" {
    
        parts := strings.Split(tmpl.Flow, "&&")
        for _, part := range parts {
            part = strings.TrimSpace(part)
            if strings.HasPrefix(part, "http(") && strings.HasSuffix(part, ")") {
                idxStr := part[5 : len(part)-1]
                idx, err := strconv.Atoi(idxStr)
                if err != nil || idx < 1 || idx > len(tmpl.Requests) {
                    return false, fmt.Errorf("invalid flow request index: %s", idxStr)
                }
                req := tmpl.Requests[idx-1]

                matched := false
                switch req.Type {
                case "http", "":
                    if canOfflineMatchRequest(req) && htmlContent != "" {
                        matched = matchOfflineHTML(htmlContent, req, tmpl, logger)
                    }
                    if !matched {
                        matched, err = matchHTTPRequest(ctx, baseURL, req, tmpl, advanced, logger)
                        if err != nil {
                            return false, err
                        }
                    }
                }

                results[idx] = matched
                if !matched {
                    return false, nil
                }
            }
        }
        return true, nil
    }
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
func checkMatchers(matchers []Matcher, condition string, ctx MatchContext, logger *logging.Logger) bool {
	if len(matchers) == 0 {
		return true
	}

	condition = strings.ToLower(condition)
	if condition == "" {
		condition = "and"
	}

	results := make([]bool, len(matchers))
	for i, m := range matchers {
		results[i] = checkSingleMatcher(m, ctx, logger)
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
func checkSingleMatcher(m Matcher, ctx MatchContext, logger *logging.Logger) bool {
	switch m.Type {
	case "status":
		if ctx.Resp == nil {
			return false
		}
		ok := slices.Contains(m.Status, ctx.Resp.StatusCode)
		if ok {
			logger.Info.Printf("Matcher type=status matched: expected %v, got %d", m.Status, ctx.Resp.StatusCode)
		}
		return ok

	case "word":
		if ctx.Resp == nil {
			return false
		}
		ok := matchWordsByPart(ctx.Resp, ctx.Body, m.Words, m.Part, m.Condition, m.NoCase)
		if ok {
			logger.Info.Printf("Matcher type=word matched: part=%s, words=%v", m.Part, m.Words)
		}
		return ok

	case "regex":
		if ctx.Resp == nil {
			return false
		}
		ok := matchRegexListByPart(ctx.Resp, ctx.Body, m.Regex, m.Part, m.NoCase)
		if ok {
			logger.Info.Printf("Matcher type=regex matched: part=%s, regex=%v", m.Part, m.Regex)
		}
		return ok

	case "size":
		if ctx.Resp == nil {
			return false
		}
		ok := matchSizeByPart(ctx.Resp, ctx.Body, m.Size, m.Part)
		if ok {
			logger.Info.Printf("Matcher type=size matched: part=%s, size=%v", m.Part, m.Size)
		}
		return ok

	case "dlength":
		if ctx.Resp == nil {
			return false
		}
		ok := matchDlengthByPart(ctx.Resp, ctx.Body, m.Condition, m.Dlength, m.Part)
		if ok {
			logger.Info.Printf("Matcher type=dlength matched: condition=%s, dlength=%v, part=%s", m.Condition, m.Dlength, m.Part)
		}
		return ok

	case "binary":
		if ctx.Resp == nil {
			return false
		}
		var binaries [][]byte
		for _, b := range m.Binary {
			binaries = append(binaries, []byte(b))
		}
		ok := matchBinaryByPart(ctx.Resp, ctx.Body, binaries, m.Part)
		if ok {
			logger.Info.Printf("Matcher type=binary matched: part=%s, binary patterns=%v", m.Part, m.Binary)
		}
		return ok

	case "xpath":
		if ctx.Body == nil {
			return false
		}
		for _, xpath := range m.XPath {
			if matchXPathByPart(ctx.Body, xpath) {
				logger.Info.Printf("Matcher type=xpath matched: xpath=%s", xpath)
				return true
			}
		}
		return false

	case "json":
		if ctx.Body == nil {
			return false
		}
		ok := matchJSONByPart(ctx.Body, m.JSONPath)
		if ok {
			logger.Info.Printf("Matcher type=json matched: jsonPath=%s", m.JSONPath)
		}
		return ok

	case "dns":
		if ctx.DNS == nil {
			return false
		}
		ok := matchDNSByPattern(ctx.DNS, m.Pattern)
		if ok {
			logger.Info.Printf("Matcher type=dns matched: pattern=%s", m.Pattern)
		}
		return ok

	case "network":
		if ctx.Network == nil {
			return false
		}
		ok := matchNetworkByPattern(ctx.Network, m.Pattern)
		if ok {
			logger.Info.Printf("Matcher type=network matched: pattern=%s", m.Pattern)
		}
		return ok

	case "headless":
		if ctx.Headless == nil {
			return false
		}
		ok := matchHeadlessByPattern(ctx.Headless, m)
		if ok {
			logger.Info.Printf("Matcher type=headless matched")
		}
		return ok

	case "dsl":
		if ctx.Resp == nil {
			return false
		}
		condition := "and"
		if m.Condition != "" {
			condition = m.Condition
		}

		results := make([]bool, 0, len(m.DSL))
		for _, expr := range m.DSL {
			matched, err := evaluateDSL(expr, ctx.Resp, ctx.Body)
			if err != nil {
				logger.Error.Printf("DSL evaluation error for expr %q: %v", expr, err)
				return false
			}
			//logger.Info.Printf("Matcher type=dsl evaluated expr=%q result=%v", expr, matched)
			results = append(results, matched)
		}

		if condition == "and" {
			for _, r := range results {
				if !r {
					return false
				}
			}
			return true
		} else if condition == "or" {
			for _, r := range results {
				if r {
					return true
				}
			}
			return false
		}
		return false

	default:
		return false
	}
}

func processExtractors(extractors []Extractor, result HTTPResult, tmpl *Template) error {
	bodyStr := string(result.Body)

	for _, extractor := range extractors {
		switch extractor.Type {
		case "regex":
			for _, pattern := range extractor.Regex {
				reFlags := ""
				if extractor.NoCase {
					reFlags = "(?i)"
				}
				re, err := regexp.Compile(reFlags + pattern)
				if err != nil {
					continue
				}

				matches := re.FindStringSubmatch(bodyStr)
				if len(matches) > 0 {
					groupIndex := 0
					if extractor.Group != "" {
						gi, err := strconv.Atoi(extractor.Group)
						if err == nil && gi < len(matches) {
							groupIndex = gi
						}
					} else if len(matches) > 1 {
						groupIndex = 1
					}
					value := matches[groupIndex]

					if extractor.Base64 {
						decoded, err := base64.StdEncoding.DecodeString(value)
						if err == nil {
							value = string(decoded)
						}
					}

					tmpl.Variables[extractor.Name] = value
					break
				}
			}

		case "xpath":
			if len(extractor.XPath) == 0 || len(bodyStr) == 0 {
				continue
			}
			for _, path := range extractor.XPath {
				vals, err := matchXPathNodesByPart([]byte(bodyStr), path)
				if err == nil && len(vals) > 0 {
					tmpl.Variables[extractor.Name] = vals[0]
					break
				}
			}

		case "jsonpath":
			if extractor.JSONPath == "" || len(bodyStr) == 0 {
				continue
			}
			vals, err := extractJSONByPath([]byte(bodyStr), extractor.JSONPath)
			if err == nil && len(vals) > 0 {
				tmpl.Variables[extractor.Name] = vals[0]
			}

		default:
		}
	}

	return nil
}
