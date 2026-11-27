// Package jsvalue reads decoded JSON values as leniently as the API always has,
// so a client sending a number as text, or the other way round, keeps working.
package jsvalue

import (
	"math"
	"strconv"
	"strings"
)

// String is JavaScript's String(value) for a decoded JSON value; null and a
// missing field are empty.
func String(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		if v == 0 {
			return "0"
		}
		if absolute := math.Abs(v); absolute >= 1e-6 && absolute < 1e21 {
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
		return strings.Replace(strconv.FormatFloat(v, 'e', -1, 64), "e-0", "e-", 1)
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = String(item)
		}
		return strings.Join(parts, ",")
	default:
		return "[object Object]"
	}
}

// Integer reads a safe whole number sent as a number or numeric text.
func Integer(value any) (int64, bool) {
	number, ok := value.(float64)
	if text, isText := value.(string); isText {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		number, ok = parsed, err == nil
	}
	if !ok || number != math.Trunc(number) || math.Abs(number) > 1<<53-1 {
		return 0, false
	}
	return int64(number), true
}
