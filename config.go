// Package nacostoolkit loads and renders local and Nacos configuration.
package nacostoolkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Format selects a configuration parser.
type Format string

const (
	JSON Format = "json"
	YAML Format = "yaml"
)

// Parse accepts source text or an already decoded JSON-shaped value. Invalid
// text and non-object YAML return an empty object.
func Parse(raw any, format Format) any {
	value := raw
	var err error
	text, fromText := raw.(string)
	if fromText {
		value, err = decode([]byte(text), format)
	}
	if format == YAML {
		if _, isObject := value.(map[string]any); !isObject {
			return map[string]any{}
		}
	}
	if err != nil || (format != JSON && format != YAML) {
		return map[string]any{}
	}
	if fromText {
		return value
	}
	return clone(value)
}

func decode(data []byte, format Format) (any, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("configuration is not UTF-8")
	}
	switch format {
	case JSON:
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		var value any
		if err := d.Decode(&value); err != nil {
			return nil, err
		}
		if len(bytes.Trim(data[d.InputOffset():], " \r\n\t")) != 0 {
			return nil, fmt.Errorf("unexpected data after JSON value")
		}
		return normalizeJSONNumbers(value)
	case YAML:
		return decodeYAML(data)
	default:
		return nil, fmt.Errorf("unsupported file format: .%s", format)
	}
}

// FindLocalConfig finds the first existing path in JSON, YAML, YML order.
func FindLocalConfig(name, dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for _, ext := range []string{".json", ".yaml", ".yml"} {
		path := name + ext
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}
	return "", false
}

// ParseConfigFile reads a file and selects JSON or YAML by extension.
func ParseConfigFile(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ext := filepath.Ext(path)
	if ext == filepath.Base(path) {
		ext = ""
	}
	ext = strings.ToLower(ext)
	if ext == ".yml" {
		ext = ".yaml"
	}
	value, err := decode(data, Format(strings.TrimPrefix(ext, ".")))
	if err != nil {
		return nil, fmt.Errorf("parse config file %q: %w", path, err)
	}
	return value, nil
}

// GetLocalConfig discovers and reads a config. It reports false for a missing
// name or file, or when the first match cannot be parsed.
func GetLocalConfig(name, dir string) (any, bool) {
	if name == "" {
		return nil, false
	}
	path, ok := FindLocalConfig(name, dir)
	if !ok {
		return nil, false
	}
	value, err := ParseConfigFile(path)
	return value, err == nil
}

// Merge recursively merges objects and replaces other values with override.
// It returns an independent value.
func Merge(base, override any) any {
	result := clone(base)
	return mergeInto(result, override)
}

func mergeInto(base, override any) any {
	dst, baseIsObject := base.(map[string]any)
	src, overrideIsObject := override.(map[string]any)
	if !baseIsObject || !overrideIsObject {
		return clone(override)
	}
	for key, value := range src {
		dst[key] = mergeInto(dst[key], value)
	}
	return dst
}

// Process parses and renders a configuration. Config keys win over external
// variables. A nil arrayFields converts cors.whitelist, an empty slice disables
// conversion.
func Process(raw any, format Format, vars map[string]any, arrayFields []string) any {
	return processValue(Parse(raw, format), vars, arrayFields)
}

// ProcessAndMerge renders custom with base and external variables, then merges
// the result on top of base.
func ProcessAndMerge(base, custom any, format Format, vars map[string]any, arrayFields []string) any {
	return Merge(base, Process(custom, format, contextWith(vars, base), arrayFields))
}

func processValue(value any, vars map[string]any, arrayFields []string) any {
	value = Render(value, contextWith(vars, value))
	if arrayFields == nil {
		arrayFields = []string{"cors.whitelist"}
	}
	object, _ := value.(map[string]any)
	for _, path := range arrayFields {
		text, _ := lookup(object, path).(string)
		if strings.Contains(text, ",") {
			parts := strings.Split(text, ",")
			items := make([]any, len(parts))
			for i, part := range parts {
				items[i] = strings.TrimSpace(part)
			}
			setNested(object, path, items)
		}
	}
	return value
}

func contextWith(vars map[string]any, value any) map[string]any {
	object, _ := value.(map[string]any)
	context := make(map[string]any, len(vars)+len(object))
	for k, v := range vars {
		context[k] = v
	}
	for k, v := range object {
		context[k] = v
	}
	return context
}

func clone(value any) any {
	switch value := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for k, v := range value {
			result[k] = clone(v)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, v := range value {
			result[i] = clone(v)
		}
		return result
	default:
		return value
	}
}

func normalizeJSONNumbers(value any) (any, error) {
	switch value := value.(type) {
	case json.Number:
		text := value.String()
		if !strings.ContainsAny(text, ".eE") && text != "-0" {
			if number, ok, err := integerNumber(text, 10); ok && err == nil {
				return number, nil
			}
		}
		number, err := value.Float64()
		if err != nil {
			return nil, err
		}
		return floatNumber(number), nil
	case map[string]any:
		for key, item := range value {
			normalized, err := normalizeJSONNumbers(item)
			if err != nil {
				return nil, err
			}
			value[key] = normalized
		}
	case []any:
		for i, item := range value {
			normalized, err := normalizeJSONNumbers(item)
			if err != nil {
				return nil, err
			}
			value[i] = normalized
		}
	}
	return value, nil
}
