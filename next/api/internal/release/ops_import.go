package release

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

const (
	importBodyLimit = 4 << 20
	importMaxRows   = 5000
)

type importRow struct {
	Line    int    `json:"line"`
	Status  string `json:"status"` // create, update, skip or error
	Label   string `json:"label"`
	Message string `json:"message,omitempty"`
}

type importReport struct {
	Kind      string      `json:"kind"`
	DryRun    bool        `json:"dry_run"`
	Committed bool        `json:"committed"`
	Created   int         `json:"created"`
	Updated   int         `json:"updated"`
	Skipped   int         `json:"skipped"`
	Errors    int         `json:"errors"`
	NewGroups []string    `json:"new_groups"`
	Rows      []importRow `json:"rows"`
}

func (report *importReport) add(row importRow) {
	switch row.Status {
	case "create":
		report.Created++
	case "update":
		report.Updated++
	case "skip":
		report.Skipped++
	default:
		report.Errors++
	}
	report.Rows = append(report.Rows, row)
}

type csvRecord struct {
	line   int
	fields map[string]string
}

// Headers from this tool's template and export, the Chinese headings other inventory exports
// use, and NetSwitch Pro's English template. Any other column, such as an export's status
// columns, is ignored.
var importHeaderAliases = map[string]string{
	"ip": "management_ip", "management_ip": "management_ip", "ip_address": "management_ip", "管理ip": "management_ip", "ip地址": "management_ip", "管理地址": "management_ip",
	"name": "name", "设备名称": "name", "名称": "name", "vendor": "vendor", "厂商": "vendor", "model": "model", "型号": "model",
	"group": "group", "group_name": "group", "分组": "group", "site": "site", "region": "site", "校区": "site", "站点": "site", "protocol": "protocol", "协议": "protocol",
	"port": "port", "ssh_port": "port", "端口": "port", "credential": "credential", "凭据": "credential", "profile": "profile", "命令模板": "profile",
	"description": "description", "描述": "description", "备注": "description",
	"handles_release": "handles_release", "负责解绑": "handles_release",
	"snmp_credential": "snmp_credential", "snmp凭据": "snmp_credential", "snmp_凭据": "snmp_credential",
}

var vendorAliases = map[string]string{
	"huawei": "huawei", "华为": "huawei", "h3c": "h3c", "华三": "h3c", "新华三": "h3c", "ruijie": "ruijie", "锐捷": "ruijie",
	"cisco": "cisco", "思科": "cisco", "other": "other", "其他": "other",
}

func readImportCSV(text string) ([]csvRecord, error) {
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(text, "\ufeff"))) // spreadsheet exports start with a BOM
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if err != nil {
		return nil, errors.New("CSV header row required")
	}
	columns := make([]string, len(header))
	for i, name := range header {
		key := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "_")
		columns[i] = importHeaderAliases[key]
	}
	var records []csvRecord
	for {
		values, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("CSV is malformed: %v", err)
		}
		line, _ := reader.FieldPos(0)
		record := csvRecord{line: line, fields: map[string]string{}}
		blank := true
		for i, value := range values {
			if i < len(columns) && columns[i] != "" {
				value = strings.TrimSpace(value)
				// An export quotes cells that a spreadsheet would read as formulas.
				if len(value) > 1 && value[0] == '\'' && strings.ContainsRune("=+-@", rune(value[1])) {
					value = value[1:]
				}
				record.fields[columns[i]] = value
				blank = blank && value == ""
			}
		}
		if !blank {
			records = append(records, record)
		}
		if len(records) > importMaxRows {
			return nil, fmt.Errorf("at most %d rows per import", importMaxRows)
		}
	}
	if len(records) == 0 {
		return nil, errors.New("CSV has no data rows")
	}
	return records, nil
}

// namedIDs maps names to IDs; a name used by several rows maps to "" and reads as ambiguous.
func namedIDs(ctx context.Context, tx *sql.Tx, query string) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var name, id string
		if err = rows.Scan(&name, &id); err != nil {
			return nil, err
		}
		if _, seen := result[name]; seen {
			result[name] = ""
		} else {
			result[name] = id
		}
	}
	return result, rows.Err()
}

func (a *AdminHandler) importAssets(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Kind                string `json:"kind"`
		CSV                 string `json:"csv"`
		DryRun              bool   `json:"dry_run"`
		DefaultCredentialID string `json:"default_credential_id"`
		DefaultProfileID    string `json:"default_profile_id"`
		Mode                string `json:"mode"` // update changes switches whose management IP exists; anything else skips them
	}
	// Networks are read from the release switches' configuration, so only switches import.
	if err := decodeLimit(r, &input, importBodyLimit); err != nil || input.Kind != "switches" {
		fail(w, 422, "kind switches and CSV text required")
		return
	}
	records, err := readImportCSV(input.CSV)
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 503, "asset store unavailable")
		return
	}
	defer tx.Rollback()
	report := importReport{Kind: input.Kind, DryRun: input.DryRun, NewGroups: []string{}, Rows: []importRow{}}
	err = importSwitches(r.Context(), tx, actor, records, input.DefaultCredentialID, input.DefaultProfileID, input.Mode == "update", &report)
	if err != nil {
		fail(w, 503, "asset store unavailable")
		return
	}
	// Rows are inserted for real either way, so a dry run reports exactly what a commit would do.
	if report.Errors == 0 && !input.DryRun && report.Created+report.Updated > 0 {
		if err = tx.Commit(); err != nil {
			fail(w, 503, "asset store unavailable")
			return
		}
		report.Committed = true
	}
	write(w, 200, report)
}

func importInsert(ctx context.Context, tx *sql.Tx, actor domain.Principal, action, label, statement string, args ...any) error {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT import_row`); err != nil {
		return err
	}
	var id string
	err := tx.QueryRowContext(ctx, statement, args...).Scan(&id)
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO bp_asset_events(actor_id,actor_email,actor_name,action,object_id,object_name) VALUES ($1,$2,$6,$3,$4,$5)`, actor.User.ID, actor.User.Email, action, id, label, actor.User.Name)
	}
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT import_row`); rollbackErr != nil {
			return rollbackErr
		}
		return fmt.Errorf("%w: %w", errImportRow, err)
	}
	_, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT import_row`)
	return err
}

var errImportRow = errors.New("row rejected by the database")

// importRejection says why the database refused a row, where people can act on it.
func importRejection(err error) string {
	if isConstraint(err, profileVendorConstraint) {
		return errProfileVendor
	}
	return "the database rejected this row"
}

// importChangeNames are the template's names for the switch columns an update can change.
var importChangeNames = map[string]string{
	"name": "name", "vendor": "vendor", "model": "model", "group_id": "group", "site": "site", "protocol": "protocol",
	"port": "port", "credential_id": "credential", "command_profile_id": "profile", "description": "description",
	"handles_release": "handles_release", "snmp_credential_id": "snmp_credential",
}

// yesNo reads a spreadsheet's yes or no cell; known is false for anything else.
func yesNo(value string) (yes, known bool) {
	switch strings.ToLower(value) {
	case "true", "yes", "y", "1", "是":
		return true, true
	case "false", "no", "n", "0", "否":
		return false, true
	}
	return false, false
}

// importSwitches creates the switches whose management IP is new. A switch that exists is
// skipped, or with update set, given the row's non-empty cells; an empty cell keeps its value.
func importSwitches(ctx context.Context, tx *sql.Tx, actor domain.Principal, records []csvRecord, defaultCredential, defaultProfile string, update bool, report *importReport) (err error) {
	credentials, err := namedIDs(ctx, tx, `SELECT name,id::text FROM bp_credentials`)
	if err != nil {
		return err
	}
	profiles, err := namedIDs(ctx, tx, `SELECT name,id::text FROM bp_command_profiles`)
	if err != nil {
		return err
	}
	profileVendor, err := namedIDs(ctx, tx, `SELECT id::text,vendor FROM bp_command_profiles`)
	if err != nil {
		return err
	}
	// A vendor with exactly one active profile maps to it; with several, to "".
	vendorProfile, err := namedIDs(ctx, tx, `SELECT vendor,id::text FROM bp_command_profiles WHERE is_active`)
	if err != nil {
		return err
	}
	groups, err := namedIDs(ctx, tx, `SELECT name,id::text FROM bp_switch_groups`)
	if err != nil {
		return err
	}
	existing, err := namedIDs(ctx, tx, `SELECT host(management_ip),id::text FROM bp_switches`)
	if err != nil {
		return err
	}
	snmpCredentials, err := namedIDs(ctx, tx, `SELECT name,id::text FROM bp_snmp_credentials`)
	if err != nil {
		return err
	}
	// Switches whose SNMP credential changed are polled at once, and any no longer monitored forget their state.
	polled := false
	defer func() {
		if err == nil && polled {
			err = forgetUnmonitored(ctx, tx)
		}
	}()
	groupID := func(name string) (string, error) {
		if name == "" || groups[name] != "" {
			return groups[name], nil
		}
		var id string
		if err := tx.QueryRowContext(ctx, `INSERT INTO bp_switch_groups(name) VALUES ($1) RETURNING id::text`, name).Scan(&id); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bp_asset_events(actor_id,actor_email,actor_name,action,object_id,object_name) VALUES ($1,$2,$5,'group.create',$3,$4)`, actor.User.ID, actor.User.Email, id, name, actor.User.Name); err != nil {
			return "", err
		}
		groups[name] = id
		report.NewGroups = append(report.NewGroups, name)
		return id, nil
	}
	seen := map[string]int{}
	for _, record := range records {
		f := record.fields
		ip := f["management_ip"]
		row := importRow{Line: record.line, Label: defaultString(f["name"], ip)}
		reject := func(message string) { row.Status, row.Message = "error", message; report.add(row) }
		vendor, knownVendor := vendorAliases[strings.ToLower(f["vendor"])]
		protocol := strings.ToLower(f["protocol"])
		var port *int
		if f["port"] != "" {
			n, err := strconv.Atoi(f["port"])
			if err != nil || n < 1 || n > 65535 {
				reject("port must be 1-65535")
				continue
			}
			port = &n
		}
		credentialID, profileID, snmpID := credentials[f["credential"]], profiles[f["profile"]], snmpCredentials[f["snmp_credential"]]
		handlesRelease, knownRole := yesNo(f["handles_release"])
		switch {
		case !validIPv4(ip):
			reject("management_ip must be one canonical IPv4 address")
			continue
		case seen[ip] != 0:
			reject(fmt.Sprintf("management_ip repeats line %d", seen[ip]))
			continue
		case f["vendor"] != "" && !knownVendor:
			reject("vendor must be huawei, h3c, ruijie, cisco or other")
			continue
		case protocol != "" && protocol != "telnet" && protocol != "ssh":
			reject("protocol must be telnet or ssh")
			continue
		case f["handles_release"] != "" && !knownRole:
			reject("handles_release must be yes or no")
			continue
		case f["credential"] != "" && !domain.IsCanonicalUUID(credentialID):
			reject("credential not found (give its exact name, or choose a default)")
			continue
		case f["profile"] != "" && !domain.IsCanonicalUUID(profileID):
			reject("command profile not found (give its exact name, or choose a default)")
			continue
		case f["snmp_credential"] != "" && !domain.IsCanonicalUUID(snmpID):
			reject("SNMP credential not found (give its exact name)")
			continue
		}
		seen[ip] = record.line
		if id := existing[ip]; id != "" {
			if !update {
				row.Status, row.Message = "skip", "a switch with this management IP already exists"
				report.add(row)
				continue
			}
			next := map[string]any{}
			for column, value := range map[string]string{"name": f["name"], "vendor": vendor, "model": f["model"], "site": f["site"],
				"protocol": protocol, "credential_id": credentialID, "command_profile_id": profileID, "description": f["description"], "snmp_credential_id": snmpID} {
				if value != "" {
					next[column] = value
				}
			}
			if port != nil {
				next["port"] = *port
			}
			if f["handles_release"] != "" {
				next["handles_release"] = handlesRelease
			}
			if f["group"] != "" {
				if next["group_id"], err = groupID(f["group"]); err != nil {
					return err
				}
			}
			changes, err := importUpdate(ctx, tx, actor, id, next)
			if err == nil {
				var touched bool
				touched, err = pollSoon(ctx, tx, id, changes)
				polled = polled || touched
			}
			if errors.Is(err, errImportRow) {
				reject(importRejection(err))
				continue
			}
			if err != nil {
				return err
			}
			if len(changes) == 0 {
				row.Status, row.Message = "skip", "already up to date"
				report.add(row)
				continue
			}
			names := []string{}
			for column := range changes {
				names = append(names, importChangeNames[column])
			}
			sort.Strings(names)
			row.Status, row.Message = "update", "changes: "+strings.Join(names, ", ")
			report.add(row)
			continue
		}
		if f["credential"] == "" {
			credentialID = defaultCredential
		}
		// A row without a profile takes the default when it is of the row's vendor, or else the
		// vendor's only active profile; a row without a vendor takes its profile's.
		if f["profile"] == "" {
			switch {
			case defaultProfile != "" && (vendor == "" || profileVendor[defaultProfile] == vendor):
				profileID = defaultProfile
			case vendor != "":
				profileID = vendorProfile[vendor]
			}
		}
		if vendor == "" {
			vendor = profileVendor[profileID]
		}
		switch {
		case f["name"] == "":
			reject("name is required")
			continue
		case !domain.IsCanonicalUUID(credentialID):
			reject("credential not found (give its exact name, or choose a default)")
			continue
		case !domain.IsCanonicalUUID(profileID) && f["profile"] == "" && vendor != "":
			reject("no single command profile for this vendor (name one, or choose a default of this vendor)")
			continue
		case !domain.IsCanonicalUUID(profileID):
			reject("command profile not found (give its exact name, or choose a default)")
			continue
		case profileVendor[profileID] != "" && profileVendor[profileID] != vendor:
			reject(errProfileVendor)
			continue
		}
		group, err := groupID(f["group"])
		if err != nil {
			return err
		}
		err = importInsert(ctx, tx, actor, "switch.import", row.Label,
			`INSERT INTO bp_switches(name,management_ip,credential_id,command_profile_id,vendor,model,site,description,group_id,protocol,port,handles_release,snmp_credential_id)
 VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,'')::uuid,$10,$11,$12,NULLIF($13,'')::uuid) RETURNING id::text`,
			row.Label, ip, credentialID, profileID, defaultString(vendor, "other"), f["model"], f["site"], f["description"], group, defaultString(protocol, "telnet"), port, handlesRelease, snmpID)
		if errors.Is(err, errImportRow) {
			reject(importRejection(err))
			continue
		}
		if err != nil {
			return err
		}
		row.Status = "create"
		report.add(row)
	}
	return nil
}

// importUpdate changes one existing switch inside a savepoint, so a rejected row leaves the
// rest of the import intact. It records the change exactly as an edit would.
func importUpdate(ctx context.Context, tx *sql.Tx, actor domain.Principal, id string, next map[string]any) (map[string]any, error) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT import_row`); err != nil {
		return nil, err
	}
	_, changes, err := updateRow(ctx, tx, actor, switchFields, id, 0, next)
	if err == nil {
		err = releaseRoleChanged(ctx, tx, id, changes)
	}
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT import_row`); rollbackErr != nil {
			return nil, rollbackErr
		}
		return nil, fmt.Errorf("%w: %w", errImportRow, err)
	}
	_, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT import_row`)
	return changes, err
}
