// package templates - utils (support functions)
package templates

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// newInsecureHTTTPClient returns HTTP client with TLS-certificate checking disabled
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
func substituteVariables(s string, vars map[string]string) string {
	for k, v := range vars {
		placeholder := fmt.Sprintf("{{%s}}", k)
		s = strings.ReplaceAll(s, placeholder, v)
	}
	return s
}

// templateMatchesHost checks if the target host matches the list in the template
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
