// package templates - utils (support functions)
package templates

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/artnikel/nuclei/internal/logging"
	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"
)

// newInsecureHTTTPClient returns HTTP client with TLS-certificate checking disabled
func newInsecureHTTPClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:    100,
		MaxConnsPerHost: 10,
		IdleConnTimeout: 30 * time.Second,
	}

	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
	}
}

// buildFullURL builds a full URL based on the base and relative paths
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

// substituteVariables replaces placeholders of the {{key}} form with values from vars
func substituteVariables(s string, vars map[string]interface{}) string {
	for k, v := range vars {
		placeholder := fmt.Sprintf("{{%s}}", k)

		switch val := v.(type) {
		case string:
			s = strings.ReplaceAll(s, placeholder, val)
		case []interface{}:
			var parts []string
			for _, item := range val {
				if strItem, ok := item.(string); ok {
					parts = append(parts, strItem)
				}
			}
			s = strings.ReplaceAll(s, placeholder, strings.Join(parts, ","))
		case []string:
			s = strings.ReplaceAll(s, placeholder, strings.Join(val, ","))
		}
	}
	return s
}

// templateMatchesHost checks if the target host matches the list in the template
func templateMatchesHost(tmpl *Template, targetHost string, logger *logging.Logger) bool {
	// Если hosts полностью отсутствует или содержит только пустые строки — считаем, что шаблон универсальный
	if len(tmpl.Hosts) == 0 || (len(tmpl.Hosts) == 1 && tmpl.Hosts[0] == "") {
		return true
	}

	for _, h := range tmpl.Hosts {
		if strings.Contains(targetHost, h) {
			return true
		}
	}

	logger.Info.Printf("Skipping template %s: host mismatch (target: %s, expected: %+v)", tmpl.ID, targetHost, tmpl.Hosts)
	return false
}


// extractHTMLTitle extracts the contents of the <title> tag from the HTML document
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

// escapeYAMLString escapes quotes for safe use in YAML
func escapeYAMLString(s string) string {
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// canOfflineMatch returns true if the matcher type supports offline matching
func canOfflineMatch(m Matcher) bool {
	switch m.Type {
	case "word", "regex":
		return true
	default:
		return false
	}
}

// canOfflineMatchRequest returns true if all matchers in the request support offline matching
func canOfflineMatchRequest(req *Request) bool {
	for _, m := range req.Matchers {
		if !canOfflineMatch(m) {
			return false
		}
	}
	return true
}

func isProfileFile(data []byte) (bool, error) {
	var prof struct {
		Severity  []string `yaml:"severity"`
		Type      []string `yaml:"type"`
		ExcludeID []string `yaml:"exclude-id"`
	}

	if err := yaml.Unmarshal(data, &prof); err != nil {
		return false, err
	}

	return len(prof.Severity) > 0 || len(prof.Type) > 0 || len(prof.ExcludeID) > 0, nil
}

var goodResultsMu sync.Mutex

func SaveGood(target, templateID string) {
	goodResultsMu.Lock()
	defer goodResultsMu.Unlock()

	f, err := os.OpenFile("goods.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("error writing to goods.txt: %v\n", err)
		return
	}
	defer f.Close()

	_, _ = fmt.Fprintf(f, "%s -> %s\n", target, templateID)
}
