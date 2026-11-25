package nacostoolkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v4"
)

func decodeYAML(data []byte) (any, error) {
	loader, err := yaml.NewLoader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var document yaml.Node
	if err := loader.Load(&document); err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, err
	}
	var extra yaml.Node
	if err := loader.Load(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple YAML documents are not supported")
		}
		return nil, err
	}
	// Count aliases by the size of the source tree, as serde_norway does.
	aliasesLeft := 100 * nodeCount(&document)
	return yamlValue(document.Content[0], 0, &aliasesLeft)
}

func nodeCount(node *yaml.Node) int {
	count := 1
	for _, child := range node.Content {
		count += nodeCount(child)
	}
	return count
}

func yamlValue(node *yaml.Node, depth int, aliasesLeft *int) (any, error) {
	if depth == 128 {
		return nil, fmt.Errorf("YAML nesting limit exceeded")
	}
	if node.Kind == yaml.AliasNode {
		(*aliasesLeft)--
		if *aliasesLeft < 0 {
			return nil, fmt.Errorf("YAML alias expansion limit exceeded")
		}
		return yamlValue(node.Alias, depth+1, aliasesLeft)
	}
	if node.Style&yaml.TaggedStyle != 0 && strings.HasPrefix(node.Tag, "!") && !strings.HasPrefix(node.Tag, "!!") {
		return nil, fmt.Errorf("YAML tag %q is not a JSON value", node.Tag)
	}

	switch node.Kind {
	case yaml.MappingNode:
		result := make(map[string]any, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind == yaml.AliasNode {
				key = key.Alias
			}
			if key.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("expected a string YAML key at line %d", key.Line)
			}
			value, err := yamlValue(node.Content[i+1], depth+1, aliasesLeft)
			if err != nil {
				return nil, err
			}
			result[key.Value] = value
		}
		return result, nil
	case yaml.SequenceNode:
		result := make([]any, len(node.Content))
		for i, child := range node.Content {
			value, err := yamlValue(child, depth+1, aliasesLeft)
			if err != nil {
				return nil, err
			}
			result[i] = value
		}
		return result, nil
	default:
		return yamlScalar(node)
	}
}

func yamlScalar(node *yaml.Node) (any, error) {
	text := node.Value
	if node.Style&yaml.TaggedStyle != 0 {
		tag := node.ShortTag()
		var value any
		var ok bool
		switch tag {
		case "!!int":
			number, parsed, err := yamlInteger(text)
			if err != nil {
				return nil, err
			}
			value, ok = number, parsed
		case "!!float":
			value, ok = yamlFloat(text)
		case "!!bool":
			value, ok = yamlBool(text)
		case "!!null":
			ok = yamlNull(text)
		default:
			return text, nil
		}
		if !ok {
			return nil, fmt.Errorf("invalid YAML %s at line %d", tag, node.Line)
		}
		return value, nil
	}
	if node.Style != 0 {
		return text, nil
	}
	if text == "" || yamlNull(text) {
		return nil, nil
	}
	if value, ok := yamlBool(text); ok {
		return value, nil
	}
	if leadingZero(text) {
		return text, nil
	}
	if value, ok, err := yamlInteger(text); ok {
		return value, err
	}
	if value, ok := yamlFloat(text); ok {
		return value, nil
	}
	return text, nil
}

func yamlNull(text string) bool {
	return text == "~" || text == "null" || text == "Null" || text == "NULL"
}

func yamlBool(text string) (bool, bool) {
	switch text {
	case "true", "True", "TRUE":
		return true, true
	case "false", "False", "FALSE":
		return false, true
	default:
		return false, false
	}
}

func leadingZero(text string) bool {
	unsigned := strings.TrimPrefix(strings.TrimPrefix(text, "+"), "-")
	return len(unsigned) > 1 && unsigned[0] == '0' && strings.Trim(unsigned, "0123456789") == ""
}

func yamlInteger(text string) (json.Number, bool, error) {
	sign := ""
	if strings.HasPrefix(text, "+") || strings.HasPrefix(text, "-") {
		sign, text = text[:1], text[1:]
	}
	if text == "" || leadingZero(text) {
		return "", false, nil
	}
	base := 10
	if len(text) > 2 && text[0] == '0' {
		switch text[1] {
		case 'b':
			text, base = text[2:], 2
		case 'o':
			text, base = text[2:], 8
		case 'x':
			text, base = text[2:], 16
		}
	}
	if strings.ContainsAny(text, "+-") {
		return "", false, nil
	}
	if sign == "-" {
		text = sign + text
	}
	return integerNumber(text, base)
}

func yamlFloat(text string) (any, bool) {
	switch text {
	case ".nan", ".NaN", ".NAN", ".inf", ".Inf", ".INF",
		"+.inf", "+.Inf", "+.INF", "-.inf", "-.Inf", "-.INF":
		return nil, true
	}
	if !strings.ContainsAny(text, "_xX") {
		if value, err := strconv.ParseFloat(text, 64); err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
			return floatNumber(value), true
		}
	}
	return nil, false
}

func integerNumber(text string, base int) (json.Number, bool, error) {
	if strings.HasPrefix(text, "-") {
		if value, err := strconv.ParseInt(text, base, 64); err == nil {
			return json.Number(strconv.FormatInt(value, 10)), true, nil
		}
	} else if value, err := strconv.ParseUint(text, base, 64); err == nil {
		return json.Number(strconv.FormatUint(value, 10)), true, nil
	}
	if value, ok := new(big.Int).SetString(text, base); ok {
		bits := value.BitLen()
		if bits < 128 || bits == 128 && (value.Sign() > 0 || value.TrailingZeroBits() == 127) {
			return "", true, fmt.Errorf("YAML number out of JSON range")
		}
	}
	return "", false, nil
}

func floatNumber(value float64) json.Number {
	magnitude := math.Abs(value)
	if magnitude >= 1e16 || magnitude > 0 && magnitude < 1e-5 {
		mantissa, exponent, _ := strings.Cut(strconv.FormatFloat(value, 'e', -1, 64), "e")
		return json.Number(mantissa + "e" + exponent[:1] + strings.TrimLeft(exponent[1:], "0"))
	}
	text := strconv.FormatFloat(value, 'f', -1, 64)
	if !strings.Contains(text, ".") {
		text += ".0"
	}
	return json.Number(text)
}
