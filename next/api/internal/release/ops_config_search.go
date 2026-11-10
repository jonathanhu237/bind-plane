package release

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// searchLinesShown bounds the lines one switch returns; every match is still counted.
	searchLinesShown = 20
	// searchSwitchesShown bounds the switches one search returns, by name.
	searchSwitchesShown = 500
	// searchLineChars cuts a returned line, such as a certificate, down to a readable length.
	searchLineChars = 500
)

type configMatch struct {
	Line    int    `json:"line"`
	Text    string `json:"text"`
	Section string `json:"section"`
}

func clip(text string) string {
	if utf8.RuneCountInString(text) <= searchLineChars {
		return text
	}
	return string([]rune(text)[:searchLineChars]) + "…"
}

// searchLines finds the matching lines of one configuration. Each match carries the section it
// sits in: the nearest line above it that starts a block (no indentation, and not a "#" or "!"
// separator), so " ip address 10.0.0.1 …" reads as part of "interface Vlanif10".
func searchLines(content string, match func(string) bool, limit int) ([]configMatch, int) {
	found, count, section := []configMatch{}, 0, ""
	for i, line := range configLines(content) {
		trimmed := strings.TrimSpace(line)
		indented := line != "" && (line[0] == ' ' || line[0] == '\t')
		if !indented && trimmed != "" && trimmed != "#" && trimmed != "!" {
			section = trimmed
		}
		if !match(line) {
			continue
		}
		count++
		if len(found) < limit {
			m := configMatch{Line: i + 1, Text: clip(line)}
			if indented {
				m.Section = clip(section)
			}
			found = append(found, m)
		}
	}
	return found, count
}

// configMatcher reads a search as plain text, matched without regard to case, or as an RE2
// pattern, which runs in linear time like compliance rules.
func configMatcher(text, mode string) (func(string) bool, error) {
	if mode == "regex" {
		re, err := regexp.Compile(text)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern: %v", err)
		}
		return re.MatchString, nil
	}
	needle := strings.ToLower(text)
	return func(line string) bool { return strings.Contains(strings.ToLower(line), needle) }, nil
}

// configSearch looks for text in the newest configuration of every switch the device list filter
// matches. It signs in to nothing: the backups are the source. Matched lines can hold secrets,
// so the search is recorded, with its text and how many switches matched, before results leave.
func (o *OpsHandler) configSearch(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, false)
	if !ok {
		return
	}
	q := r.URL.Query()
	text, mode := strings.TrimSpace(q.Get("text")), q.Get("mode")
	if n := utf8.RuneCountInString(text); n < 2 || n > 200 {
		fail(w, 422, "search text must be 2 to 200 characters")
		return
	}
	match, err := configMatcher(text, mode)
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	ctx, args := r.Context(), switchFilterArgs(q)
	var withoutBackup int64
	if err = o.db.QueryRowContext(ctx, `SELECT count(*) FROM bp_switches s
 WHERE `+switchFilter+` AND NOT EXISTS (SELECT 1 FROM bp_config_backups b WHERE b.switch_id=s.id)`, args...).Scan(&withoutBackup); err != nil {
		o.unavailable(w, err)
		return
	}
	rows, err := o.db.QueryContext(ctx, `SELECT DISTINCT ON (b.switch_id) b.id,s.id::text,s.name,host(s.management_ip),g.name,b.captured_at,b.encrypted_content
 FROM bp_config_backups b JOIN bp_switches s ON s.id=b.switch_id LEFT JOIN bp_switch_groups g ON g.id=s.group_id
 WHERE `+switchFilter+` ORDER BY b.switch_id,b.captured_at DESC,b.id DESC`, args...)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	defer rows.Close()
	type result struct {
		BackupID   int64         `json:"backup_id"`
		SwitchID   string        `json:"switch_id"`
		SwitchName string        `json:"switch_name"`
		IP         string        `json:"management_ip"`
		GroupName  *string       `json:"group_name"`
		CapturedAt time.Time     `json:"captured_at"`
		Count      int           `json:"count"`
		Matches    []configMatch `json:"matches"`
	}
	results, searched, lines := []result{}, 0, 0
	for rows.Next() {
		var res result
		var sealed string
		if err = rows.Scan(&res.BackupID, &res.SwitchID, &res.SwitchName, &res.IP, &res.GroupName, &res.CapturedAt, &sealed); err != nil {
			o.unavailable(w, err)
			return
		}
		content, err := openConfig(o.key, sealed)
		if err != nil {
			o.unavailable(w, err)
			return
		}
		searched++
		if res.Matches, res.Count = searchLines(content, match, searchLinesShown); res.Count > 0 {
			lines += res.Count
			results = append(results, res)
		}
	}
	if err = rows.Err(); err != nil {
		o.unavailable(w, err)
		return
	}
	sort.Slice(results, func(i, j int) bool { return results[i].SwitchName < results[j].SwitchName })
	matched := len(results)
	if matched > searchSwitchesShown {
		results = results[:searchSwitchesShown]
	}
	if err = o.auditRead(ctx, actor, "config.search", actor.User.ID, fmt.Sprintf("%s (%d switches)", text, matched)); err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, map[string]any{"searched": searched, "without_backup": withoutBackup, "matched": matched, "lines": lines,
		"truncated": matched > len(results), "results": results})
}
