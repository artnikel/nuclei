// package templates - for checking matcher type
package templates

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/antchfx/htmlquery"
)

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
	doc, err := htmlquery.Parse(bytes.NewReader(body))
	if err != nil {
		return false
	}
	nodes := htmlquery.Find(doc, xpathExpr)
	return len(nodes) > 0
}

// getJSONValue retrieves a value from JSON at path
func getJSONValue(body []byte, path string) interface{} {
	var data interface{}
	err := json.Unmarshal(body, &data)
	if err != nil {
		return nil
	}
	parts := strings.Split(path, ".")
	cur := data
	for _, p := range parts {
		switch v := cur.(type) {
		case map[string]any:
			cur = v[p]
		case []any:
			idx, err := strconv.Atoi(p)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil
			}
			cur = v[idx]
		default:
			return nil
		}
	}
	return cur
}

// matchJSONByPart проверяет наличие значения по JSON-пути в теле ответа
func matchJSONByPart(body []byte, jsonPath string) bool {
	val := getJSONValue(body, jsonPath)
	return val != nil
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
			if !strings.Contains(text, w) {
				return false
			}
		}
		return true
	}

	for _, w := range words {
		if strings.Contains(text, w) {
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


