// package templates - for checking matcher type
package templates

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Knetic/govaluate"
	"github.com/antchfx/htmlquery"
	"github.com/yalp/jsonpath"
	"golang.org/x/net/html"
)

func flexibleContains(text, pattern string) bool {
	normalizeQuotes := func(s string) string {
		s = strings.ReplaceAll(s, `\"`, `"`)
		s = strings.ReplaceAll(s, `\'`, `'`)
		s = strings.ReplaceAll(s, `"`, `"`)
		s = strings.ReplaceAll(s, `'`, `"`)
		return s
	}

	normalizeSpaces := func(s string) string {
		s = strings.ReplaceAll(s, "\n", "")
		s = strings.ReplaceAll(s, "\r", "")
		s = strings.ReplaceAll(s, "\t", "")
		s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
		s = strings.TrimSpace(s)
		return s
	}

	normalizedText := normalizeSpaces(normalizeQuotes(text))
	normalizedPattern := normalizeSpaces(normalizeQuotes(pattern))

	return strings.Contains(normalizedText, normalizedPattern)
}

// matchBinaryByPart checks for the presence of a binary pattern in the specified part of the response
func matchBinaryByPart(resp *http.Response, body []byte, patterns [][]byte, part string) bool {
	var data []byte

	switch strings.ToLower(part) {
	case "body", "":
		data = body
	case "header":
		var headers []string
		for k, v := range resp.Header {
			headers = append(headers, k+": "+strings.Join(v, ","))
		}
		data = []byte(strings.Join(headers, "\n"))
	case "all":
		var headers []string
		for k, v := range resp.Header {
			headers = append(headers, k+": "+strings.Join(v, ","))
		}
		data = append(body, []byte("\n"+strings.Join(headers, "\n"))...)
	default:
		data = body
	}

	for _, pattern := range patterns {
		if bytes.Contains(data, pattern) {
			return true
		}
	}
	return false
}

// matchDlengthByPart compares the length of the data in the answer part with the specified condition
func matchDlengthByPart(resp *http.Response, body []byte, operator string, length int, part string) bool {
	var data string

	switch strings.ToLower(part) {
	case "body", "":
		data = string(body)
	case "header":
		var headers []string
		for k, v := range resp.Header {
			headers = append(headers, k+": "+strings.Join(v, ","))
		}
		data = strings.Join(headers, "\n")
	case "all":
		var headers []string
		for k, v := range resp.Header {
			headers = append(headers, k+": "+strings.Join(v, ","))
		}
		data = string(body) + "\n" + strings.Join(headers, "\n")
	default:
		data = string(body)
	}

	dataLen := len(data)

	switch operator {
	case "==", "=":
		return dataLen == length
	case "!=":
		return dataLen != length
	case ">":
		return dataLen > length
	case ">=":
		return dataLen >= length
	case "<":
		return dataLen < length
	case "<=":
		return dataLen <= length
	default:
		return dataLen == length
	}
}

// matchXPathByPart checks for XPath nodes in the body of the HTML response
func matchXPathByPart(body []byte, xpathExpr string) bool {
	nodes, err := matchXPathNodesByPart(body, xpathExpr)
	if err != nil {
		return false
	}
	return len(nodes) > 0
}

func matchXPathNodesByPart(body []byte, xpathExpr string) ([]*html.Node, error) {
	doc, err := htmlquery.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	nodes := htmlquery.Find(doc, xpathExpr)
	return nodes, nil
}

func matchJSONByPart(body []byte, jsonPathExpr string) bool {
	vals, err := extractJSONByPath(body, jsonPathExpr)
	if err != nil {
		return false
	}
	return len(vals) > 0
}

func extractJSONByPath(body []byte, jsonPathExpr string) ([]interface{}, error) {
	var data interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	vals, err := jsonpath.Read(data, jsonPathExpr)
	if err != nil {
		return nil, err
	}

	switch v := vals.(type) {
	case []interface{}:
		return v, nil
	default:
		return []interface{}{v}, nil
	}
}

// matchJSONByPart checks if the value exists along the JSON path in the response body
func matchWordsByPart(resp *http.Response, body []byte, words []string, part, condition string, noCase bool) bool {
	var text string

	switch part {
	case "body", "":
		text = string(body)
	case "header":
		var headers []string
		for k, v := range resp.Header {
			headers = append(headers, k+": "+strings.Join(v, ","))
		}
		text = strings.Join(headers, "\n")
	case "all":
		var headers []string
		for k, v := range resp.Header {
			headers = append(headers, k+": "+strings.Join(v, ","))
		}
		text = string(body) + "\n" + strings.Join(headers, "\n")
	case "status":
		text = fmt.Sprintf("%d", resp.StatusCode)
	default:
		text = string(body)
	}

	if noCase {
		text = strings.ToLower(text)
		for i, w := range words {
			words[i] = strings.ToLower(w)
		}
	}

	if condition == "and" {
		for _, w := range words {
			if !flexibleContains(text, w) {
				return false
			}
		}
		return true
	}

	for _, w := range words {
		if flexibleContains(text, w) {
			return true
		}
	}
	return false
}

// matchRegexListByPart checks for a match to the regular expression in the answer part
func matchRegexListByPart(resp *http.Response, body []byte, regexList []string, part string, noCase bool) bool {
	var text string

	switch part {
	case "body", "":
		text = string(body)
	case "header":
		var headers []string
		for k, v := range resp.Header {
			headers = append(headers, k+": "+strings.Join(v, ","))
		}
		text = strings.Join(headers, "\n")
	case "all":
		var headers []string
		for k, v := range resp.Header {
			headers = append(headers, k+": "+strings.Join(v, ","))
		}
		text = string(body) + "\n" + strings.Join(headers, "\n")
	case "status":
		text = fmt.Sprintf("%d", resp.StatusCode)
	default:
		text = string(body)
	}

	for _, regexStr := range regexList {
		prefix := ""
		if noCase {
			prefix = "(?i)"
		}
		re, err := regexp.Compile(prefix + regexStr)
		if err != nil {
			continue
		}
		if re.MatchString(text) {
			return true
		}
	}

	return false
}

// matchSizeByPart compares the size of the specified response part with the specified value
func matchSizeByPart(resp *http.Response, body []byte, size int, part string) bool {
	var length int
	switch part {
	case "body", "":
		length = len(body)
	case "header":
		length = 0
		for k, v := range resp.Header {
			length += len(k) + len(strings.Join(v, ",")) + 2
		}
	case "all":
		length = len(body)
		for k, v := range resp.Header {
			length += len(k) + len(strings.Join(v, ",")) + 2
		}
	default:
		length = len(body)
	}
	return length == size
}

// matchDNSByPattern checks if any DNS record contains the pattern (case-insensitive)
func matchDNSByPattern(dnsResp *DNSResponse, pattern string) bool {
	if dnsResp == nil {
		return false
	}

	for _, record := range dnsResp.Records {
		if strings.Contains(strings.ToLower(record), strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// matchNetworkByPattern checks if the network response data contains the pattern bytes
func matchNetworkByPattern(nw *NetworkResponse, pattern string) bool {
	return bytes.Contains(nw.Data, []byte(pattern))
}

// matchHeadlessByPattern checks if the headless response HTML matches words or regex patterns
func matchHeadlessByPattern(resp *HeadlessResponse, m Matcher) bool {
	html := resp.HTML
	if html == "" {
		return false
	}
	// check if any word from matcher is in HTML (case-insensitive)
	if len(m.Words) > 0 {
		for _, w := range m.Words {
			if strings.Contains(strings.ToLower(html), strings.ToLower(w)) {
				return true
			}
		}
		return false
	}
	// check if any regex pattern from matcher matches HTML
	if len(m.Regex) > 0 {
		for _, pattern := range m.Regex {
			re, err := regexp.Compile(pattern)
			if err != nil {
				continue
			}
			if re.MatchString(html) {
				return true
			}
		}
		return false
	}

	return false
}

func evaluateDSL(dsl string, resp *http.Response, body []byte) (bool, error) {
	bodyStr := string(body)
	statusCode := resp.StatusCode

	parameters := map[string]interface{}{
		"status_code": statusCode,
		"body":        bodyStr,
	}

	functions := map[string]govaluate.ExpressionFunction{
		"contains": func(args ...interface{}) (interface{}, error) {
			if len(args) != 2 {
				return false, nil
			}
			haystack, ok1 := args[0].(string)
			needle, ok2 := args[1].(string)
			if !ok1 || !ok2 {
				return false, nil
			}

			result := flexibleContains(haystack, needle)

			return result, nil
		},
		"regex": func(args ...interface{}) (interface{}, error) {
			if len(args) != 2 {
				return false, nil
			}
			pattern, ok1 := args[0].(string)
			subject, ok2 := args[1].(string)
			if !ok1 || !ok2 {
				return false, nil
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return false, nil
			}
			result := re.MatchString(subject)
			return result, nil
		},
	}

	expression, err := govaluate.NewEvaluableExpressionWithFunctions(dsl, functions)
	if err != nil {
		return false, err
	}

	result, err := expression.Evaluate(parameters)
	if err != nil {
		return false, err
	}

	boolResult, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("expression did not evaluate to bool")
	}

	return boolResult, nil
}
