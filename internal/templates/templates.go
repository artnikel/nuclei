package templates

import (
	"bytes"
	"context"
	"crypto/tls"
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

	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"
)

type Template struct {
	ID             string            `yaml:"id"`
	Info           Info              `yaml:"info"`
	Tags           Tags              `yaml:"tags,omitempty"`
	Authors        []string          `yaml:"authors,omitempty"`
	Severity       string            `yaml:"severity,omitempty"`
	Description    string            `yaml:"description,omitempty"`
	Reference      []string          `yaml:"reference,omitempty"`
	Classification map[string]string `yaml:"classification,omitempty"`
	Metadata       map[string]string `yaml:"metadata,omitempty"`
	Variables      map[string]string `yaml:"variables,omitempty"`

	RequestsRaw []*Request `yaml:"requests,omitempty"`
	HTTPRaw     []*Request `yaml:"http,omitempty"`

	Requests []*Request `yaml:"-"`

	Hosts []string `yaml:"hosts,omitempty"`
}

type Info struct {
	Name        string `yaml:"name"`
	Author      string `yaml:"author"`
	Severity    string `yaml:"severity"`
	Description string `yaml:"description,omitempty"`
	Tags        Tags   `yaml:"tags,omitempty"`
}

type Request struct {
	Method            string            `yaml:"method"`
	Path              []string          `yaml:"path"`
	Headers           map[string]string `yaml:"headers,omitempty"`
	Matchers          []Matcher         `yaml:"matchers,omitempty"`
	MatchersCondition string            `yaml:"matchers-condition,omitempty"`
	Extractors        []Extractor       `yaml:"extractors,omitempty"`
	Attack            *Attack           `yaml:"attack,omitempty"`
}

type Matcher struct {
	Type      string   `yaml:"type"`
	Part      string   `yaml:"part,omitempty"`
	Words     []string `yaml:"words,omitempty"`
	Status    []int    `yaml:"status,omitempty"`
	Condition string   `yaml:"condition,omitempty"`
	Regex     string   `yaml:"regex,omitempty"`
	Size      int      `yaml:"size,omitempty"`
	Dlength   int      `yaml:"dlength,omitempty"`
	Binary    string   `yaml:"binary,omitempty"`
	XPath     string   `yaml:"xpath,omitempty"`
	JSONPath  string   `yaml:"jsonpath,omitempty"`
	NoCase    bool     `yaml:"nocase,omitempty"`
}

type Extractor struct {
	Type     string `yaml:"type"`
	Part     string `yaml:"part,omitempty"`
	Group    string `yaml:"group,omitempty"`
	Regex    string `yaml:"regex,omitempty"`
	Name     string `yaml:"name,omitempty"`
	NoCase   bool   `yaml:"nocase,omitempty"`
	XPath    string `yaml:"xpath,omitempty"`
	JSONPath string `yaml:"jsonpath,omitempty"`
	Base64   bool   `yaml:"base64,omitempty"`
}

type Attack struct {
	Payloads map[string][]string `yaml:"payloads,omitempty"`
	Headers  map[string]string   `yaml:"headers,omitempty"`
	Raw      string              `yaml:"raw,omitempty"`
}

type Tags []string

func (t *Tags) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		parts := strings.Split(value.Value, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		*t = parts
	case yaml.SequenceNode:
		var tags []string
		if err := value.Decode(&tags); err != nil {
			return err
		}
		*t = tags
	default:
		return fmt.Errorf("unexpected yaml node kind for Tags: %v", value.Kind)
	}
	return nil
}

func LoadTemplates(dir string) ([]*Template, error) {
	var templates []*Template
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !(strings.HasSuffix(d.Name(), ".yaml") || strings.HasSuffix(d.Name(), ".yml")) {
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

func templateMatchesHost(tmpl *Template, targetHost string) bool {
	if len(tmpl.Hosts) == 0 {
		return true
	}
	for _, h := range tmpl.Hosts {
		if strings.Contains(targetHost, h) {
			return true
		}
	}
	return false
}

func FindMatchingTemplates(ctx context.Context, targetURL string, templatesDir string, timeout time.Duration) ([]*Template, error) {
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
	client := &http.Client{
		Timeout: timeout,
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, tmpl := range templates {
		if !templateMatchesHost(tmpl, targetHost) {
			continue
		}

		wg.Add(1)
		go func(t *Template) {
			defer wg.Done()

			matches, err := MatchTemplate(ctx, client, targetURL, t)
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

func newInsecureHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
}

func MatchTemplate(ctx context.Context, client *http.Client, baseURL string, tmpl *Template) (bool, error) {
	if len(tmpl.Requests) == 0 {
		return false, fmt.Errorf("template %s has no requests", tmpl.ID)
	}

	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil {
		return false, fmt.Errorf("invalid target url: %w", err)
	}

	for _, req := range tmpl.Requests {
		method := req.Method
		if method == "" {
			method = http.MethodGet
		}

		for _, p := range req.Path {
			pathWithVars := substituteVariables(p, tmpl.Variables)
			fullURL := buildFullURL(parsedBaseURL, pathWithVars)

			client := newInsecureHTTPClient(10 * time.Second)

			httpReq, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
			if err != nil {
				continue
			}

			for k, v := range req.Headers {
				httpReq.Header.Set(k, substituteVariables(v, tmpl.Variables))
			}

			resp, err := client.Do(httpReq)
			if err != nil {
				fmt.Printf("Request error for %s: %v\n", fullURL, err)
				continue
			}

			bodyBytes, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				fmt.Printf("Read body error for %s: %v\n", fullURL, err)
				continue
			}

			matched := checkMatchers(req.Matchers, req.MatchersCondition, resp, bodyBytes)
			fmt.Printf("Template %s, request %s: matched=%v, status=%d\n", tmpl.ID, fullURL, matched, resp.StatusCode)
			if matched {
				return true, nil
			}
		}
	}

	return false, nil
}

func buildFullURL(base *url.URL, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	u := *base
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	return u.String()
}

func substituteVariables(s string, vars map[string]string) string {
	for k, v := range vars {
		placeholder := fmt.Sprintf("{{%s}}", k)
		s = strings.ReplaceAll(s, placeholder, v)
	}
	return s
}

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

func checkSingleMatcher(m Matcher, resp *http.Response, body []byte) bool {
	switch m.Type {
	case "status":
		for _, st := range m.Status {
			if resp.StatusCode == st {
				return true
			}
		}
		return false
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

func GenerateTemplate(targetURL string) string {
	client := newInsecureHTTPClient(10 * time.Second)
	resp, err := client.Get(targetURL)
	if err != nil {
		return fmt.Sprintf("# Failed to request %s: %s\n", targetURL, err)
	}
	defer resp.Body.Close()

	tpl, err := GenerateTemplateFromResponse(targetURL, resp)
	if err != nil {
		return fmt.Sprintf("# Failed to generate template from %s: %s\n", targetURL, err)
	}

	return tpl
}

func GenerateTemplateFromResponse(targetURL string, resp *http.Response) (string, error) {
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return "", err
	}

	baseURL := "{{BaseURL}}"
	path := parsedURL.Path
	if path == "" {
		path = "/"
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		bodyBytes = []byte{}
	}
	title := extractHTMLTitle(bytes.NewReader(bodyBytes))

	serverHeader := resp.Header.Get("Server")
	contentType := resp.Header.Get("Content-Type")

	var buf bytes.Buffer
	buf.WriteString("id: autogenerated-template\n")
	buf.WriteString("info:\n")
	buf.WriteString("  name: Autogenerated Template\n")
	buf.WriteString("  author: scanner\n")
	buf.WriteString("  severity: info\n")
	buf.WriteString("  tags:\n")
	buf.WriteString("    - autogenerated\n\n")

	buf.WriteString("hosts:\n")
	buf.WriteString(fmt.Sprintf("  - %s\n\n", parsedURL.Hostname()))

	buf.WriteString("requests:\n")
	buf.WriteString("  - method: GET\n")
	buf.WriteString("    path:\n")
	buf.WriteString(fmt.Sprintf("      - \"%s%s\"\n", baseURL, path))
	if parsedURL.RawQuery != "" {
		buf.WriteString(fmt.Sprintf("      - \"%s%s?%s\"\n", baseURL, path, parsedURL.RawQuery))
	}

	buf.WriteString("\n    matchers:\n")
	buf.WriteString(fmt.Sprintf("      - type: status\n        status:\n          - %d\n", resp.StatusCode))

	if serverHeader != "" {
		buf.WriteString("      - type: word\n        part: header\n        words:\n")
		buf.WriteString(fmt.Sprintf("          - \"%s\"\n", escapeYAMLString(serverHeader)))
	}
	if contentType != "" {
		buf.WriteString("      - type: word\n        part: header\n        words:\n")
		buf.WriteString(fmt.Sprintf("          - \"%s\"\n", escapeYAMLString(contentType)))
	}
	if title != "" {
		buf.WriteString("      - type: word\n        part: body\n        words:\n")
		buf.WriteString(fmt.Sprintf("          - \"%s\"\n", escapeYAMLString(title)))
	}

	return buf.String(), nil
}

func extractHTMLTitle(r io.Reader) string {
	doc, err := html.Parse(r)
	if err != nil {
		return ""
	}

	var title string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
			title = n.FirstChild.Data
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
	return title
}

func escapeYAMLString(s string) string {
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}
