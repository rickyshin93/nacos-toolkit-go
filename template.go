package nacostoolkit

import (
	"bytes"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"
)

var placeholder = regexp.MustCompile(`\$\{([^}]+)\}`)

const maxRenderDepth = 5

// ContainsTemplate reports whether text contains a ${...} placeholder.
func ContainsTemplate(text string) bool {
	return placeholder.MatchString(text)
}

// RenderText substitutes dot-path references over at most five passes. Missing
// and null values keep their original placeholders.
func RenderText(text string, vars map[string]any) string {
	for range maxRenderDepth {
		rendered := substitute(text, func(path, original string) string {
			return asText(lookup(vars, path), original)
		})
		if rendered == text {
			break
		}
		text = rendered
	}
	return text
}

// Render expands placeholders in a JSON-shaped value and returns a copy.
// Whole-value references keep their type outside direct string array entries.
func Render(value any, vars map[string]any) any {
	params := make(map[string]bool)
	collectParams(value, params)
	if len(params) == 0 {
		return clone(value)
	}

	context := clone(vars).(map[string]any)
	for _, path := range slices.Sorted(maps.Keys(params)) {
		resolved := lookup(vars, path)
		if text, ok := resolved.(string); ok {
			resolved = resolveParam(text, vars, map[string]bool{path: true})
		}
		setNested(context, path, resolved)
	}
	return renderValue(value, context, 0)
}

func collectParams(value any, params map[string]bool) {
	switch value := value.(type) {
	case string:
		for _, match := range placeholder.FindAllStringSubmatch(value, -1) {
			params[match[1]] = true
		}
	case map[string]any:
		for _, child := range value {
			collectParams(child, params)
		}
	case []any:
		for _, child := range value {
			collectParams(child, params)
		}
	}
}

func resolveParam(text string, vars map[string]any, visiting map[string]bool) string {
	return substitute(text, func(path, original string) string {
		if visiting[path] {
			return original
		}
		child := asText(lookup(vars, path), original)
		if child == original {
			return original
		}
		visiting[path] = true
		resolved := resolveParam(child, vars, visiting)
		delete(visiting, path)
		return resolved
	})
}

func substitute(text string, replace func(path, original string) string) string {
	return placeholder.ReplaceAllStringFunc(text, func(match string) string {
		return replace(match[2:len(match)-1], match)
	})
}

func renderValue(value any, vars map[string]any, depth int) any {
	switch value := value.(type) {
	case string:
		match := placeholder.FindStringSubmatchIndex(value)
		if match != nil && match[0] == 0 && match[1] == len(value) {
			resolved := lookup(vars, value[match[2]:match[3]])
			switch resolved.(type) {
			case map[string]any, []any:
				if depth < maxRenderDepth {
					return renderValue(resolved, vars, depth+1)
				}
				return clone(resolved)
			case nil, string:
			default:
				return resolved
			}
		}
		return RenderText(value, vars)
	case map[string]any:
		result := make(map[string]any, len(value))
		for k, v := range value {
			result[k] = renderValue(v, vars, depth)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			switch item := item.(type) {
			case string:
				result[i] = RenderText(item, vars)
			case map[string]any:
				result[i] = renderValue(item, vars, depth)
			default:
				result[i] = clone(item)
			}
		}
		return result
	default:
		return value
	}
}

func lookup(object map[string]any, path string) any {
	for {
		key, rest, nested := strings.Cut(path, ".")
		value := object[key]
		if !nested {
			return value
		}
		object, _ = value.(map[string]any)
		path = rest
	}
}

func setNested(object map[string]any, path string, value any) {
	for {
		key, rest, nested := strings.Cut(path, ".")
		if !nested {
			object[key] = clone(value)
			return
		}
		child, ok := object[key].(map[string]any)
		if !ok {
			child = map[string]any{}
			object[key] = child
		}
		object, path = child, rest
	}
}

func asText(value any, fallback string) string {
	switch value := value.(type) {
	case nil:
		return fallback
	case string:
		return value
	default:
		var buf bytes.Buffer
		encoder := json.NewEncoder(&buf)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(value); err != nil {
			return fallback
		}
		return strings.TrimSuffix(buf.String(), "\n")
	}
}
