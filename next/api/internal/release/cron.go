package release

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronSchedule is a standard five-field cron expression: minute hour day-of-month month day-of-week.
type CronSchedule struct {
	minute, hour, dom, month, dow uint64
	// Like Vixie cron, when both day fields are restricted a day matches either one.
	domRestricted, dowRestricted bool
}

var errCronFields = errors.New("cron expression needs five fields: minute hour day month weekday")

func ParseCron(expression string) (CronSchedule, error) {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return CronSchedule{}, errCronFields
	}
	var c CronSchedule
	bounds := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	sets := [5]*uint64{&c.minute, &c.hour, &c.dom, &c.month, &c.dow}
	names := [5]string{"minute", "hour", "day", "month", "weekday"}
	for i, field := range fields {
		set, err := parseCronField(field, bounds[i][0], bounds[i][1])
		if err != nil {
			return CronSchedule{}, fmt.Errorf("cron %s field: %w", names[i], err)
		}
		*sets[i] = set
	}
	if c.dow&(1<<7) != 0 { // 7 is another name for Sunday
		c.dow = c.dow&^(1<<7) | 1
	}
	c.domRestricted = !strings.HasPrefix(fields[2], "*")
	c.dowRestricted = !strings.HasPrefix(fields[4], "*")
	return c, nil
}

func parseCronField(field string, low, high int) (uint64, error) {
	var set uint64
	for _, part := range strings.Split(field, ",") {
		start, end, step := low, high, 1
		rangePart := part
		if before, after, found := strings.Cut(part, "/"); found {
			n, err := strconv.Atoi(after)
			if err != nil || n < 1 {
				return 0, fmt.Errorf("invalid step %q", part)
			}
			rangePart, step = before, n
		}
		switch {
		case rangePart == "*":
		case strings.Contains(rangePart, "-"):
			a, b, _ := strings.Cut(rangePart, "-")
			var errA, errB error
			start, errA = strconv.Atoi(a)
			end, errB = strconv.Atoi(b)
			if errA != nil || errB != nil || start > end {
				return 0, fmt.Errorf("invalid range %q", part)
			}
		default:
			n, err := strconv.Atoi(rangePart)
			if err != nil {
				return 0, fmt.Errorf("invalid value %q", part)
			}
			start = n
			if !strings.Contains(part, "/") {
				end = n
			}
		}
		if start < low || end > high {
			return 0, fmt.Errorf("%q is outside %d-%d", part, low, high)
		}
		for v := start; v <= end; v += step {
			set |= 1 << uint(v)
		}
	}
	return set, nil
}

func (c CronSchedule) dayMatches(t time.Time) bool {
	dom := c.dom&(1<<uint(t.Day())) != 0
	dow := c.dow&(1<<uint(t.Weekday())) != 0
	if c.domRestricted && c.dowRestricted {
		return dom || dow
	}
	return dom && dow
}

// Next returns the first matching minute strictly after `after`, in after's location,
// or the zero time when nothing matches within five years (for example 30 February).
func (c CronSchedule) Next(after time.Time) time.Time {
	loc := after.Location()
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		switch {
		case c.month&(1<<uint(t.Month())) == 0:
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc)
		case !c.dayMatches(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
		case c.hour&(1<<uint(t.Hour())) == 0:
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, loc)
		case c.minute&(1<<uint(t.Minute())) == 0:
			t = t.Add(time.Minute)
		default:
			return t
		}
	}
	return time.Time{}
}
