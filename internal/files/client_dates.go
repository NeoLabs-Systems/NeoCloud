package files

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Dates a client sends with an upload: when the photo was taken or the file
// created, and when it last changed on the device. Either ISO 8601 or epoch
// milliseconds, kept as wall-clock "YYYY-MM-DDTHH:MM:SS" like EXIF dates: an
// ISO time as written, epoch milliseconds in the server's time zone. Anything
// unreadable, from the first day of 1970 (how devices say "unknown") or more
// than a day ahead is dropped, never an error.

const wallClockLayout = "2006-01-02T15:04:05"

var (
	isoDatePattern   = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2})(?::(\d{2})(?:\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?$`)
	epochDatePattern = regexp.MustCompile(`^-?\d{1,16}$`)
)

func plausibleClientDate(instant time.Time) bool {
	return instant.After(time.Unix(86400, 0)) && !instant.After(time.Now().Add(24*time.Hour))
}

func clientDate(raw string) *string {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 40 {
		return nil
	}
	if epochDatePattern.MatchString(value) {
		milliseconds, err := strconv.ParseInt(value, 10, 64)
		if err != nil || !plausibleClientDate(time.UnixMilli(milliseconds)) {
			return nil
		}
		wall := time.UnixMilli(milliseconds).Local().Format(wallClockLayout)
		return &wall
	}
	match := isoDatePattern.FindStringSubmatch(value)
	if match == nil {
		return nil
	}
	seconds := match[3]
	if seconds == "" {
		seconds = "00"
	}
	wall := match[1] + "T" + match[2] + ":" + seconds
	zone := time.Local
	if match[4] != "" {
		zone = time.UTC
		if match[4] != "Z" {
			digits := strings.ReplaceAll(match[4][1:], ":", "")
			hours, _ := strconv.Atoi(digits[:2])
			minutes, _ := strconv.Atoi(digits[2:])
			offset := (hours*60 + minutes) * 60
			if match[4][0] == '-' {
				offset = -offset
			}
			zone = time.FixedZone("", offset)
		}
	}
	instant, err := time.ParseInLocation(wallClockLayout, wall, zone)
	if err != nil || !plausibleClientDate(instant) {
		return nil
	}
	return &wall
}
