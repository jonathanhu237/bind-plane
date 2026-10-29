package release

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
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
	Status  string `json:"status"` // create, skip or error
	Label   string `json:"label"`
	Message string `json:"message,omitempty"`
}

type importReport struct {
	Kind      string      `json:"kind"`
	DryRun    bool        `json:"dry_run"`
	Committed bool        `json:"committed"`
	Created   int         `json:"created"`
	Skipped   int         `json:"skipped"`
	Errors    int         `json:"errors"`
	NewGroups []string    `json:"new_groups"`
	Rows      []importRow `json:"rows"`
}

func (report *importReport) add(row importRow) {
	switch row.Status {
	case "create":
		report.Created++
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

// Headers from this tool's template, plus the Chinese headings other inventory exports use.
var importHeaderAliases = map[string]string{
	"ip": "management_ip", "management_ip": "management_ip", "管理ip": "management_ip", "ip地址": "management_ip",
	"name": "name", "设备名称": "name", "名称": "name", "vendor": "vendor", "厂商": "vendor", "model": "model", "型号": "model",
	"group": "group", "分组": "group", "site": "site", "校区": "site", "站点": "site", "protocol": "protocol", "协议": "protocol",
	"port": "port", "端口": "port", "credential": "credential", "凭据": "credential", "profile": "profile", "命令模板": "profile",
	"description": "description", "描述": "description", "备注": "description", "cidr": "cidr", "网段": "cidr",
	"switch": "switch", "交换机": "switch", "vlan": "vlan", "validated": "validated", "已验证": "validated",
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
				record.fields[columns[i]] = strings.TrimSpace(value)
				blank = blank && strings.TrimSpace(value) == ""
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
	}
	if err := decodeLimit(r, &input, importBodyLimit); err != nil || input.Kind != "switches" && input.Kind != "networks" {
		fail(w, 422, "kind (switches or networks) and CSV text required")
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
	if input.Kind == "switches" {
		err = importSwitches(r.Context(), tx, actor, records, input.DefaultCredentialID, input.DefaultProfileID, &report)
	} else {
		err = importNetworks(r.Context(), tx, actor, records, &report)
	}
	if err != nil {
		fail(w, 503, "asset store unavailable")
		return
	}
	// Rows are inserted for real either way, so a dry run reports exactly what a commit would do.
	if report.Errors == 0 && !input.DryRun && report.Created > 0 {
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
		_, err = tx.ExecContext(ctx, `INSERT INTO bp_asset_events(actor_id,actor_email,action,object_id,object_name) VALUES ($1,$2,$3,$4,$5)`, actor.User.ID, actor.User.Email, action, id, label)
	}
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT import_row`); rollbackErr != nil {
			return rollbackErr
		}
		return fmt.Errorf("%w: %v", errImportRow, err)
	}
	_, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT import_row`)
	return err
}

var errImportRow = errors.New("row rejected by the database")

func importSwitches(ctx context.Context, tx *sql.Tx, actor domain.Principal, records []csvRecord, defaultCredential, defaultProfile string, report *importReport) error {
	credentials, err := namedIDs(ctx, tx, `SELECT name,id::text FROM bp_credentials`)
	if err != nil {
		return err
	}
	profiles, err := namedIDs(ctx, tx, `SELECT name,id::text FROM bp_command_profiles`)
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
	seen := map[string]int{}
	for _, record := range records {
		f := record.fields
		row := importRow{Line: record.line, Label: f["name"]}
		reject := func(message string) { row.Status, row.Message = "error", message; report.add(row) }
		ip := f["management_ip"]
		vendor, knownVendor := vendorAliases[strings.ToLower(defaultString(f["vendor"], "other"))]
		protocol := strings.ToLower(defaultString(f["protocol"], "telnet"))
		var port *int
		if f["port"] != "" {
			n, err := strconv.Atoi(f["port"])
			if err != nil || n < 1 || n > 65535 {
				reject("port must be 1-65535")
				continue
			}
			port = &n
		}
		credentialID, profileID := credentials[f["credential"]], profiles[f["profile"]]
		if f["credential"] == "" {
			credentialID = defaultCredential
		}
		if f["profile"] == "" {
			profileID = defaultProfile
		}
		switch {
		case row.Label == "":
			reject("name is required")
			continue
		case !validIPv4(ip):
			reject("management_ip must be one canonical IPv4 address")
			continue
		case seen[ip] != 0:
			reject(fmt.Sprintf("management_ip repeats line %d", seen[ip]))
			continue
		case !knownVendor:
			reject("vendor must be huawei, h3c, ruijie, cisco or other")
			continue
		case protocol != "telnet" && protocol != "ssh":
			reject("protocol must be telnet or ssh")
			continue
		case !domain.IsCanonicalUUID(credentialID):
			reject("credential not found (give its exact name, or choose a default)")
			continue
		case !domain.IsCanonicalUUID(profileID):
			reject("command profile not found (give its exact name, or choose a default)")
			continue
		}
		seen[ip] = record.line
		if existing[ip] != "" {
			row.Status, row.Message = "skip", "a switch with this management IP already exists"
			report.add(row)
			continue
		}
		groupID := ""
		if name := f["group"]; name != "" {
			if groupID = groups[name]; groupID == "" {
				if err = tx.QueryRowContext(ctx, `INSERT INTO bp_switch_groups(name) VALUES ($1) RETURNING id::text`, name).Scan(&groupID); err != nil {
					return err
				}
				if _, err = tx.ExecContext(ctx, `INSERT INTO bp_asset_events(actor_id,actor_email,action,object_id,object_name) VALUES ($1,$2,'group.create',$3,$4)`, actor.User.ID, actor.User.Email, groupID, name); err != nil {
					return err
				}
				groups[name] = groupID
				report.NewGroups = append(report.NewGroups, name)
			}
		}
		err = importInsert(ctx, tx, actor, "switch.import", row.Label,
			`INSERT INTO bp_switches(name,management_ip,credential_id,command_profile_id,vendor,model,site,description,group_id,protocol,port)
 VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,'')::uuid,$10,$11) RETURNING id::text`,
			row.Label, ip, credentialID, profileID, vendor, f["model"], f["site"], f["description"], groupID, protocol, port)
		if errors.Is(err, errImportRow) {
			reject("the database rejected this row")
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

func importNetworks(ctx context.Context, tx *sql.Tx, actor domain.Principal, records []csvRecord, report *importReport) error {
	byIP, err := namedIDs(ctx, tx, `SELECT host(management_ip),id::text FROM bp_switches`)
	if err != nil {
		return err
	}
	byName, err := namedIDs(ctx, tx, `SELECT name,id::text FROM bp_switches`)
	if err != nil {
		return err
	}
	existing, err := namedIDs(ctx, tx, `SELECT switch_id::text||' '||cidr::text,id::text FROM bp_networks`)
	if err != nil {
		return err
	}
	owners, err := namedIDs(ctx, tx, `SELECT cidr::text,switch_id::text FROM bp_networks`)
	if err != nil {
		return err
	}
	seen := map[string]int{}
	for _, record := range records {
		f := record.fields
		row := importRow{Line: record.line, Label: f["cidr"]}
		reject := func(message string) { row.Status, row.Message = "error", message; report.add(row) }
		switchID, named := byIP[f["switch"]], false
		if switchID == "" {
			switchID, named = byName[f["switch"]]
		}
		validated := false
		switch strings.ToLower(f["validated"]) {
		case "", "false", "no", "0", "否":
		case "true", "yes", "1", "是":
			validated = true
		default:
			reject("validated must be true or false")
			continue
		}
		prefix, prefixErr := netip.ParsePrefix(row.Label)
		switch {
		case prefixErr != nil || !prefix.Addr().Is4():
			reject("cidr must be an IPv4 network such as 10.0.0.0/24")
			continue
		case prefix.Masked() != prefix:
			reject("cidr has host bits set; did you mean " + prefix.Masked().String() + "?")
			continue
		case named && switchID == "":
			reject("several switches share this name; use the management IP")
			continue
		case switchID == "":
			reject("switch not found (give its management IP or exact name)")
			continue
		case seen[switchID+" "+row.Label] != 0:
			reject(fmt.Sprintf("network repeats line %d", seen[switchID+" "+row.Label]))
			continue
		}
		seen[switchID+" "+row.Label] = record.line
		if existing[switchID+" "+row.Label] != "" {
			row.Status, row.Message = "skip", "this switch already owns this network"
			report.add(row)
			continue
		}
		if owner, taken := owners[row.Label]; taken && owner != switchID {
			row.Message = "another switch also owns this network; resolution stays ambiguous until corrected"
		}
		err = importInsert(ctx, tx, actor, "network.import", row.Label,
			`INSERT INTO bp_networks(switch_id,cidr,is_validated,vlan,description) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,'')) RETURNING id::text`,
			switchID, row.Label, validated, f["vlan"], f["description"])
		if errors.Is(err, errImportRow) {
			reject("the database rejected this row")
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
