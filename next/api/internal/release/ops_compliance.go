package release

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

var ruleFields = assetKind{"bp_compliance_rules", "rule.update", "name", []assetField{
	{column: "name", read: "name"}, {column: "description", read: "description"}, {column: "pattern", read: "pattern"},
	{column: "mode", read: "mode"}, {column: "severity", read: "severity"}, {column: "vendor", read: "vendor"},
	{column: "group_id", read: "group_id::text"}, {column: "is_enabled", read: "is_enabled"},
}}

var (
	ruleModes  = map[string]bool{"must_contain": true, "must_not_contain": true}
	severities = map[string]bool{"info": true, "warning": true, "critical": true}
)

// compileRule uses RE2, whose matching time is linear in the input, so an admin's pattern
// cannot stall the evaluator on a large configuration. ^ and $ match at line boundaries.
func compileRule(pattern string) (*regexp.Regexp, error) {
	if strings.TrimSpace(pattern) == "" || len(pattern) > 2000 {
		return nil, errors.New("pattern must be 1-2000 characters")
	}
	re, err := regexp.Compile("(?m)" + pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %v", err)
	}
	return re, nil
}

// evaluateRule reports whether content satisfies the rule, with the first matching line as evidence.
func evaluateRule(re *regexp.Regexp, mode, content string) (bool, string) {
	loc := re.FindStringIndex(content)
	if loc == nil {
		if mode == "must_contain" {
			return false, "Pattern not found"
		}
		return true, ""
	}
	start := strings.LastIndexByte(content[:loc[0]], '\n') + 1
	end := strings.IndexByte(content[loc[0]:], '\n')
	if end < 0 {
		end = len(content)
	} else {
		end += loc[0]
	}
	excerpt := strings.TrimSpace(content[start:end])
	if utf8.RuneCountInString(excerpt) > 160 {
		excerpt = string([]rune(excerpt)[:160]) + "…"
	}
	return mode == "must_contain", fmt.Sprintf("Line %d: %s", strings.Count(content[:loc[0]], "\n")+1, excerpt)
}

func (o *OpsHandler) rules(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	items, err := o.rows(r.Context(), []string{"id", "name", "description", "pattern", "mode", "severity", "vendor", "group_id", "group_name", "is_enabled", "version", "passed", "failed"},
		`SELECT ru.id::text,ru.name,ru.description,ru.pattern,ru.mode,ru.severity,ru.vendor,ru.group_id::text,g.name,ru.is_enabled,ru.version,
 (SELECT count(*) FROM bp_compliance_results r WHERE r.rule_id=ru.id AND r.passed),(SELECT count(*) FROM bp_compliance_results r WHERE r.rule_id=ru.id AND NOT r.passed)
 FROM bp_compliance_rules ru LEFT JOIN bp_switch_groups g ON g.id=ru.group_id ORDER BY ru.name`)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, items)
}

func (o *OpsHandler) createRule(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Pattern     string `json:"pattern"`
		Mode        string `json:"mode"`
		Severity    string `json:"severity"`
		Vendor      string `json:"vendor"`
		GroupID     string `json:"group_id"`
		IsEnabled   *bool  `json:"is_enabled"`
	}
	err := decode(r, &input)
	name, severity := strings.TrimSpace(input.Name), defaultString(input.Severity, "warning")
	if err != nil || name == "" || !ruleModes[input.Mode] || !severities[severity] || input.Vendor != "" && !switchVendors[input.Vendor] ||
		input.GroupID != "" && !domain.IsCanonicalUUID(input.GroupID) {
		fail(w, 422, "name, mode, severity and optional vendor or group required")
		return
	}
	if _, err = compileRule(input.Pattern); err != nil {
		fail(w, 422, err.Error())
		return
	}
	id, err := o.createAudited(r.Context(), actor, "rule.create", name,
		`INSERT INTO bp_compliance_rules(name,description,pattern,mode,severity,vendor,group_id,is_enabled)
 VALUES ($1,NULLIF($2,''),$3,$4,$5,NULLIF($6,''),NULLIF($7,'')::uuid,$8) RETURNING id::text`,
		name, strings.TrimSpace(input.Description), input.Pattern, input.Mode, severity, input.Vendor, input.GroupID, input.IsEnabled == nil || *input.IsEnabled)
	if err != nil {
		fail(w, 409, "rule could not be saved (duplicate name or missing group)")
		return
	}
	write(w, 201, map[string]any{"id": id, "name": name})
}

func (o *OpsHandler) editRule(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Version     int     `json:"version"`
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Pattern     *string `json:"pattern"`
		Mode        *string `json:"mode"`
		Severity    *string `json:"severity"`
		Vendor      *string `json:"vendor"`   // empty applies the rule to every vendor
		GroupID     *string `json:"group_id"` // empty applies the rule to every group
		IsEnabled   *bool   `json:"is_enabled"`
	}
	next := map[string]any{}
	if err := decode(r, &input); err != nil || !requiredText(next, "name", input.Name) || input.Mode != nil && !ruleModes[*input.Mode] ||
		input.Severity != nil && !severities[*input.Severity] || input.Vendor != nil && *input.Vendor != "" && !switchVendors[*input.Vendor] ||
		input.GroupID != nil && *input.GroupID != "" && !domain.IsCanonicalUUID(*input.GroupID) {
		fail(w, 422, "invalid rule")
		return
	}
	if input.Pattern != nil {
		if _, err := compileRule(*input.Pattern); err != nil {
			fail(w, 422, err.Error())
			return
		}
		next["pattern"] = *input.Pattern
	}
	for column, value := range map[string]*string{"mode": input.Mode, "severity": input.Severity} {
		if value != nil {
			next[column] = *value
		}
	}
	optionalText(next, "description", input.Description)
	optionalText(next, "vendor", input.Vendor)
	optionalText(next, "group_id", input.GroupID)
	optionalBool(next, "is_enabled", input.IsEnabled)
	o.applyEdit(w, r, actor, ruleFields, input.Version, next)
}

func (o *OpsHandler) results(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	q := r.URL.Query()
	status := q.Get("status")
	if status != "passed" && status != "all" {
		status = "failed"
	}
	limit, offset := pageArgs(r)
	items, err := o.rows(r.Context(), []string{"switch_id", "switch_name", "management_ip", "rule_id", "rule_name", "severity", "passed", "detail", "evaluated_at", "backup_id", "total"},
		`SELECT r.switch_id::text,s.name,host(s.management_ip),r.rule_id::text,ru.name,ru.severity,r.passed,r.detail,r.evaluated_at,r.backup_id,count(*) OVER()
 FROM bp_compliance_results r JOIN bp_switches s ON s.id=r.switch_id JOIN bp_compliance_rules ru ON ru.id=r.rule_id
 WHERE ($1='all' OR $1='failed' AND NOT r.passed OR $1='passed' AND r.passed) AND ($2='' OR r.rule_id::text=$2)
 AND ($3='' OR s.name ILIKE '%'||$3||'%' OR host(s.management_ip) LIKE $3||'%')
 ORDER BY r.passed,CASE ru.severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END,s.name,ru.name LIMIT $4 OFFSET $5`,
		status, q.Get("rule"), strings.TrimSpace(q.Get("q")), limit, offset)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	var passed, failed, switchesEvaluated, switchesFailing int64
	if err = o.db.QueryRowContext(r.Context(), `SELECT count(*) FILTER (WHERE passed),count(*) FILTER (WHERE NOT passed),count(DISTINCT switch_id),count(DISTINCT switch_id) FILTER (WHERE NOT passed) FROM bp_compliance_results`).
		Scan(&passed, &failed, &switchesEvaluated, &switchesFailing); err != nil {
		o.unavailable(w, err)
		return
	}
	total, items := paged(items)
	write(w, 200, map[string]any{"items": items, "total": total, "page_size": limit,
		"summary": map[string]int64{"passed": passed, "failed": failed, "switches_evaluated": switchesEvaluated, "switches_failing": switchesFailing}})
}

// RunComplianceEvaluator checks the latest configuration of every enabled switch against
// the rules in scope whenever a newer backup or rule version appears.
func (o *OpsHandler) RunComplianceEvaluator(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if _, err := o.evaluateCompliance(ctx); err != nil && ctx.Err() == nil {
			log.Printf("compliance evaluator: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func upsertAlert(ctx context.Context, db *sql.DB, kind, severity, switchID string, ruleID any, message, key string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO bp_alerts(kind,severity,switch_id,rule_id,message,dedupe_key) VALUES ($1,$2,$3,$4,$5,$6)
 ON CONFLICT (dedupe_key) WHERE status<>'resolved' DO UPDATE SET last_seen_at=now(),message=EXCLUDED.message,severity=EXCLUDED.severity`,
		kind, severity, switchID, ruleID, message, key)
	return err
}

func resolveAlert(ctx context.Context, db *sql.DB, key string) error {
	_, err := db.ExecContext(ctx, `UPDATE bp_alerts SET status='resolved',resolved_at=now() WHERE dedupe_key=$1 AND status<>'resolved'`, key)
	return err
}

func (o *OpsHandler) evaluateCompliance(ctx context.Context) (int, error) {
	// Results for disabled or out-of-scope pairs no longer describe anything; neither do their alerts.
	if _, err := o.db.ExecContext(ctx, `DELETE FROM bp_compliance_results r USING bp_compliance_rules ru, bp_switches s
 WHERE r.rule_id=ru.id AND r.switch_id=s.id AND (NOT ru.is_enabled OR NOT s.is_enabled
 OR ru.vendor IS NOT NULL AND ru.vendor<>s.vendor OR ru.group_id IS NOT NULL AND ru.group_id IS DISTINCT FROM s.group_id)`); err != nil {
		return 0, err
	}
	if _, err := o.db.ExecContext(ctx, `UPDATE bp_alerts a SET status='resolved',resolved_at=now() WHERE a.kind='compliance_violation' AND a.status<>'resolved'
 AND NOT EXISTS (SELECT 1 FROM bp_compliance_results r JOIN bp_compliance_rules ru ON ru.id=r.rule_id
 WHERE r.switch_id=a.switch_id AND r.rule_id=a.rule_id AND NOT r.passed AND ru.severity='critical')`); err != nil {
		return 0, err
	}
	rows, err := o.db.QueryContext(ctx, `WITH latest AS (SELECT DISTINCT ON (switch_id) id,switch_id FROM bp_config_backups ORDER BY switch_id,captured_at DESC,id DESC)
 SELECT l.id,l.switch_id::text,ru.id::text,ru.version,ru.pattern,ru.mode,ru.severity,ru.name
 FROM latest l JOIN bp_switches s ON s.id=l.switch_id AND s.is_enabled
 JOIN bp_compliance_rules ru ON ru.is_enabled AND (ru.vendor IS NULL OR ru.vendor=s.vendor) AND (ru.group_id IS NULL OR ru.group_id=s.group_id)
 LEFT JOIN bp_compliance_results r ON r.switch_id=l.switch_id AND r.rule_id=ru.id
 WHERE r.switch_id IS NULL OR r.backup_id<>l.id OR r.rule_version<>ru.version
 ORDER BY l.id LIMIT 5000`)
	if err != nil {
		return 0, err
	}
	type work struct {
		backupID                          int64
		switchID, ruleID                  string
		ruleVersion                       int
		pattern, mode, severity, ruleName string
	}
	var pending []work
	for rows.Next() {
		var item work
		if err = rows.Scan(&item.backupID, &item.switchID, &item.ruleID, &item.ruleVersion, &item.pattern, &item.mode, &item.severity, &item.ruleName); err != nil {
			rows.Close()
			return 0, err
		}
		pending = append(pending, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	var cachedID int64
	var content string
	var openErr error
	for n, item := range pending {
		if item.backupID != cachedID {
			var sealed string
			if err = o.db.QueryRowContext(ctx, `SELECT encrypted_content FROM bp_config_backups WHERE id=$1`, item.backupID).Scan(&sealed); err != nil {
				return n, err
			}
			cachedID = item.backupID
			content, openErr = openConfig(o.key, sealed)
		}
		passed, detail := false, "Configuration could not be decrypted"
		if openErr == nil {
			if re, compileErr := compileRule(item.pattern); compileErr != nil {
				detail = compileErr.Error()
			} else {
				passed, detail = evaluateRule(re, item.mode, content)
			}
		}
		if _, err = o.db.ExecContext(ctx, `INSERT INTO bp_compliance_results(switch_id,rule_id,backup_id,rule_version,passed,detail,evaluated_at)
 VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),now()) ON CONFLICT (switch_id,rule_id) DO UPDATE SET backup_id=EXCLUDED.backup_id,
 rule_version=EXCLUDED.rule_version,passed=EXCLUDED.passed,detail=EXCLUDED.detail,evaluated_at=now()`,
			item.switchID, item.ruleID, item.backupID, item.ruleVersion, passed, detail); err != nil {
			return n, err
		}
		key := "compliance:" + item.switchID + ":" + item.ruleID
		switch {
		case item.severity == "critical" && !passed:
			err = upsertAlert(ctx, o.db, "compliance_violation", "critical", item.switchID, item.ruleID, fmt.Sprintf("Rule %q failed: %s", item.ruleName, detail), key)
		case item.severity == "critical":
			err = resolveAlert(ctx, o.db, key)
		}
		if err != nil {
			return n, err
		}
	}
	return len(pending), nil
}
