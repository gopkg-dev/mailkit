package providerutil

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

const (
	HTTPStatusOK      = 200
	HTTPStatusCreated = 201
)

var OTPCodePattern = regexp.MustCompile(`\b(\d{6})\b`)

func NormalizeHydraMessages(payload any) []map[string]any {
	switch typedPayload := payload.(type) {
	case []any:
		return NormalizeItems(typedPayload)
	case map[string]any:
		if items, ok := typedPayload["hydra:member"].([]any); ok {
			return NormalizeItems(items)
		}
		if items, ok := typedPayload["messages"].([]any); ok {
			return NormalizeItems(items)
		}
		if items, ok := typedPayload["items"].([]any); ok {
			return NormalizeItems(items)
		}
	}
	return nil
}

func NormalizeItems(items []any) []map[string]any {
	normalizedItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if ok {
			normalizedItems = append(normalizedItems, itemMap)
		}
	}
	return normalizedItems
}

func NormalizeMessageID(values ...any) string {
	for _, rawValue := range values {
		value := strings.TrimSpace(AsString(rawValue))
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "/") {
			return path.Base(value)
		}
		return value
	}
	return ""
}

func MapValue(value any, key string) any {
	valueMap, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return valueMap[key]
}

func IsBoolFalse(value any) bool {
	booleanValue, ok := value.(bool)
	return ok && !booleanValue
}

func IsBoolTrue(value any) bool {
	booleanValue, ok := value.(bool)
	return ok && booleanValue
}

func AsString(value any) string {
	switch typedValue := value.(type) {
	case string:
		return typedValue
	case nil:
		return ""
	default:
		return fmt.Sprint(typedValue)
	}
}

func AsSlice(value any) []any {
	switch typedValue := value.(type) {
	case []any:
		return typedValue
	case nil:
		return nil
	default:
		return nil
	}
}
