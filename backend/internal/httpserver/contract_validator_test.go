package httpserver

import (
	"fmt"
	"math"
	"time"
)

// Validate composed response shapes as well as their siblings; a successful
// oneOf branch must not bypass the enclosing object's field validation.
func wireContractError(value any, schema map[string]any) error {
	for _, keyword := range []string{"anyOf", "oneOf"} {
		if choices, ok := schema[keyword].([]any); ok {
			matches := 0
			for _, choice := range choices {
				if wireContractError(value, choice.(map[string]any)) == nil {
					matches++
				}
			}
			if matches == 0 || keyword == "oneOf" && matches != 1 {
				return fmt.Errorf("%s matched %d alternatives", keyword, matches)
			}
		}
	}
	if required, ok := schema["required"].([]string); ok && len(required) > 0 {
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("required fields on %T", value)
		}
		for _, key := range required {
			if _, ok := object[key]; !ok {
				return fmt.Errorf("missing required %s", key)
			}
		}
	}
	switch schema["type"] {
	case "null":
		if value != nil {
			return fmt.Errorf("expected null got %T", value)
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object got %T", value)
		}
		properties, defined := schema["properties"].(map[string]any)
		for key, val := range object {
			child, ok := properties[key]
			if !ok {
				if additional, allowed := schema["additionalProperties"].(map[string]any); allowed {
					child = additional
				} else if defined {
					return fmt.Errorf("undocumented field %s", key)
				} else {
					continue
				}
			}
			if err := wireContractError(val, child.(map[string]any)); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	case "array":
		values, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expected array got %T", value)
		}
		for i, val := range values {
			if err := wireContractError(val, schema["items"].(map[string]any)); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string got %T", value)
		}
		if schema["format"] == "date-time" {
			if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
				return err
			}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean got %T", value)
		}
	case "integer", "number":
		number, ok := value.(float64)
		if !ok {
			return fmt.Errorf("expected number got %T", value)
		}
		if schema["type"] == "integer" && math.Trunc(number) != number {
			return fmt.Errorf("expected integer got %v", number)
		}
		if minimum, ok := schema["minimum"].(int); ok && number < float64(minimum) {
			return fmt.Errorf("number below minimum")
		}
	}
	return nil
}
