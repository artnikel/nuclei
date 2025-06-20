// package templates - utils (support functions)
package templates

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
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

var goodResultsMu sync.Mutex

// newInsecureHTTPClient creates a new HTTP client with custom timeouts and TLS settings
func newInsecureHTTPClient(advanced *AdvancedSettingsChecker) *http.Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   advanced.ConnectionTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: advanced.ReadTimeout,
		DisableKeepAlives:     false, // Enable keep-alive for better performance
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	return &http.Client{
		Transport: transport,
		Timeout:   advanced.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow up to 10 redirects
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			return nil
		},
	}
}

// buildFullURL builds a full URL based on the base and relative paths
func buildFullURL(base *url.URL, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}

	ref, err := url.Parse(path)
	if err != nil {
		// Если path не парсится как URL, возвращаем базу + path с добавлением слэша
		return base.Scheme + "://" + base.Host + "/" + strings.TrimLeft(path, "/")
	}

	return base.ResolveReference(ref).String()
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

func SaveGood(target string) {
	goodResultsMu.Lock()
	defer goodResultsMu.Unlock()

	f, err := os.OpenFile("goods.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("error writing to goods.txt: %v\n", err)
		return
	}
	defer f.Close()

	_, _ = fmt.Fprintf(f, "%s \n", target)
}

func normalizeURL(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return rawurl
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}