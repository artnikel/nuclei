// package templates - provides the data models used in the application
package templates

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type Template struct {
	ID               string            `yaml:"id"`
	Info             Info              `yaml:"info"`
	Tags             Tags              `yaml:"tags,omitempty"`
	Authors          []string          `yaml:"authors,omitempty"`
	Severity         string            `yaml:"severity,omitempty"`
	Description      string            `yaml:"description,omitempty"`
	Reference        []string          `yaml:"reference,omitempty"`
	Classification   map[string]string `yaml:"classification,omitempty"`
	Metadata         map[string]string `yaml:"metadata,omitempty"`
	Variables        map[string]string `yaml:"variables,omitempty"`
	StopAtFirstMatch bool              `yaml:"stop-at-first-match,omitempty"`
	RequestCondition string            `yaml:"req-condition,omitempty"`

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
	Method            string                 `yaml:"method"`
	Path              []string               `yaml:"path"`
	Headers           map[string]string      `yaml:"headers,omitempty"`
	Matchers          []Matcher              `yaml:"matchers,omitempty"`
	MatchersCondition string                 `yaml:"matchers-condition,omitempty"`
	Extractors        []Extractor            `yaml:"extractors,omitempty"`
	Attack            string                 `yaml:"attack,omitempty"`
	Payloads          map[string][]string    `yaml:"payloads,omitempty"`
	Pipeline          bool                   `yaml:"pipeline,omitempty"`
	Options           map[string]interface{} `yaml:"options,omitempty"`
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
	Type     string   `yaml:"type"`
	Part     string   `yaml:"part,omitempty"`
	Group    string   `yaml:"group,omitempty"`
	Regex    []string `yaml:"regex,omitempty"`
	Name     string   `yaml:"name,omitempty"`
	NoCase   bool     `yaml:"nocase,omitempty"`
	XPath    string   `yaml:"xpath,omitempty"`
	JSONPath string   `yaml:"jsonpath,omitempty"`
	Base64   bool     `yaml:"base64,omitempty"`
}

type Tags []string

// UnmarshalYAML implements custom parsing of the Tags field from YAML (string or list)
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
