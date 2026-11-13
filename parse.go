package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	minutesPerHour = 60
	today          = "hoje"
	yesterday      = "ontem"
	isoDay         = "2006-01-02"
	brDay          = "02/01/2006"
	timestampShown = "02/01/2006 15:04"
	// activityValueLimit keeps one history line readable when the change is a
	// whole description.
	activityValueLimit = 80
)

var (
	durationPattern = regexp.MustCompile(`^(?:(\d+)h)?\s*(?:(\d+)m)?$`)

	errDuration = errors.New(`tempo inválido: use o formato "1h 30m", "2h" ou "45m"`)
	errDay      = errors.New(`data inválida: use "hoje", "ontem", 31/12/2026 ou 2026-12-31`)

	// activityValueKeys are tried in order: the first one present names the entity.
	activityValueKeys = []string{"idReadable", "name", "fullName", "presentation", "login", "text"}
)

func parseDuration(typed string) (int, error) {
	m := durationPattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(typed)))
	if m == nil {
		return 0, errDuration
	}
	hours, _ := strconv.Atoi(m[1])
	minutes, _ := strconv.Atoi(m[2])
	total := hours*minutesPerHour + minutes
	if total <= 0 {
		return 0, errDuration
	}
	return total, nil
}

func formatMinutes(total int) string {
	hours, minutes := total/minutesPerHour, total%minutesPerHour
	switch {
	case hours == 0:
		return fmt.Sprintf("%dm", minutes)
	case minutes == 0:
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

// parseDay answers midnight UTC of the day typed, which is how YouTrack
// stores the date of a work item.
func parseDay(typed string, now time.Time) (time.Time, error) {
	typed = strings.ToLower(strings.TrimSpace(typed))
	day := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	switch typed {
	case "", today:
		return day(now), nil
	case yesterday:
		return day(now.AddDate(0, 0, -1)), nil
	}
	for _, layout := range []string{isoDay, brDay} {
		if t, err := time.Parse(layout, typed); err == nil {
			return day(t), nil
		}
	}
	return time.Time{}, errDay
}

func formatMillis(ms int64) string {
	return time.UnixMilli(ms).Format(timestampShown)
}

// formatDay reads the day in UTC, where parseDay put it: the local zone
// would show the day before west of Greenwich.
func formatDay(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(brDay)
}

// activityValue reads what an activity added or removed: a string, an entity
// or a list of entities, depending on the category.
func activityValue(raw json.RawMessage) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	text := describe(value)
	if runes := []rune(text); len(runes) > activityValueLimit {
		return string(runes[:activityValueLimit]) + "…"
	}
	return text
}

func describe(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s := describe(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		for _, key := range activityValueKeys {
			if s, ok := v[key].(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}
