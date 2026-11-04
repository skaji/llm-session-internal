// Extraction helpers adapted from github.com/skaji/llm-session-search.
package session

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	maxIndexedStringBytes = 2 << 20
	sourceCodex           = "codex"
	sourceClaude          = "claude"
)

type extractedLine struct {
	text, role, phase, cwd, title string
	timestamps                    []time.Time
	forceTitle                    bool
}

func extractSessionLine(source string, line []byte) extractedLine {
	switch source {
	case sourceCodex:
		return extractCodexJSONLine(line)
	case sourceClaude:
		return extractClaudeJSONLine(line)
	default:
		return extractedLine{}
	}
}

func decodeJSONLine(line []byte) (any, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || !json.Valid(line) {
		return nil, false
	}

	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	return value, true
}

func isBase64DataURL(value string) bool {
	return strings.HasPrefix(value, "data:") && strings.Contains(value[:min(len(value), 128)], ";base64,")
}

func extractMetadata(key string, value any, depth int, result *extractedLine) {
	switch value := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for childKey := range value {
			keys = append(keys, childKey)
		}
		slices.Sort(keys)
		for _, childKey := range keys {
			extractMetadata(childKey, value[childKey], depth+1, result)
		}
	case []any:
		for _, child := range value {
			extractMetadata(key, child, depth+1, result)
		}
	case string:
		if depth <= 2 && isTimestampKey(key) {
			if timestamp, ok := parseTimestamp(value); ok {
				result.timestamps = append(result.timestamps, timestamp)
			}
		}
		switch {
		case depth <= 2 && strings.EqualFold(key, "phase"):
			if result.phase == "" {
				result.phase = value
			}
		case depth <= 2 && strings.EqualFold(key, "cwd"):
			if result.cwd == "" {
				result.cwd = value
			}
		}
	case json.Number:
		if depth <= 2 && isTimestampKey(key) {
			if timestamp, ok := parseNumericTimestamp(value.String()); ok {
				result.timestamps = append(result.timestamps, timestamp)
			}
		}
	}
}

func isTimestampKey(key string) bool {
	switch strings.ToLower(key) {
	case "timestamp", "started_at", "updated_at", "created_at", "completed_at", "create_time":
		return true
	default:
		return false
	}
}

func parseTimestamp(value string) (time.Time, bool) {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return validTimestamp(parsed)
		}
	}
	return parseNumericTimestamp(value)
}

func parseNumericTimestamp(value string) (time.Time, bool) {
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || number <= 0 {
		return time.Time{}, false
	}
	var seconds float64
	switch {
	case number > 1e17:
		seconds = number / 1e9
	case number > 1e14:
		seconds = number / 1e6
	case number > 1e11:
		seconds = number / 1e3
	default:
		seconds = number
	}
	whole := int64(seconds)
	nanos := int64((seconds - float64(whole)) * 1e9)
	return validTimestamp(time.Unix(whole, nanos))
}

func validTimestamp(value time.Time) (time.Time, bool) {
	year := value.Year()
	return value, year >= 2000 && year <= 2100
}
