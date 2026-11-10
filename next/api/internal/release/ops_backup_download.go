package release

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// errNothingToExport is returned when none of the chosen switches has a configuration version yet.
var errNothingToExport = errors.New("no configuration backups to export")

// maxExportIDs bounds a download of chosen switches; a larger set is exported by filter instead.
const maxExportIDs = 1000

// configFileName names a configuration file after its switch, address and capture time, so files
// from many switches sort and stay apart. Characters that file systems reject become underscores.
func configFileName(name, ip string, captured time.Time, loc *time.Location) string {
	safe := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if safe == "" || strings.Trim(safe, ".") == "" {
		safe = "switch"
	}
	return fmt.Sprintf("%s_%s_%s.cfg", safe, ip, captured.In(loc).Format("20060102-1504"))
}

func attachment(name string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}

// downloadBackup sends one configuration version as a text file. Like viewing it, downloading is
// recorded against the switch first and fails closed without that record.
func (o *OpsHandler) downloadBackup(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, false)
	if !ok {
		return
	}
	b, err := o.openBackup(r.Context(), r.PathValue("id"))
	if err == ErrNotFound {
		fail(w, 404, err.Error())
		return
	}
	var ip string
	if err == nil {
		err = o.db.QueryRowContext(r.Context(), `SELECT host(management_ip) FROM bp_switches WHERE id=$1`, b.switchID).Scan(&ip)
	}
	if err == nil {
		err = o.auditRead(r.Context(), actor, "config.download", b.switchID, fmt.Sprintf("%s (version %d)", b.switchName, b.id))
	}
	if err != nil {
		o.unavailable(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", attachment(configFileName(b.switchName, ip, b.capturedAt, o.loc)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write([]byte(b.content))
}

type latestBackup struct {
	id                 int64
	switchID, name, ip string
	capturedAt         time.Time
}

// exportIDs reads the optional comma-separated switch IDs that narrow an export to chosen switches.
func exportIDs(raw string) ([]string, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, true
	}
	ids := strings.Split(raw, ",")
	if len(ids) > maxExportIDs {
		return nil, false
	}
	for _, id := range ids {
		if !domain.IsCanonicalUUID(id) {
			return nil, false
		}
	}
	return ids, true
}

// exportBackups sends the newest configuration version of every switch the device list filter
// matches (narrowed to `ids` when given) as one zip. Each switch's history records the export
// before any configuration leaves; switches without a version are left out.
func (o *OpsHandler) exportBackups(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, false)
	if !ok {
		return
	}
	ids, valid := exportIDs(r.URL.Query().Get("ids"))
	if !valid {
		fail(w, 422, fmt.Sprintf("ids must be at most %d switch IDs", maxExportIDs))
		return
	}
	ctx := r.Context()
	latest, err := o.latestBackups(ctx, append(switchFilterArgs(r.URL.Query()), strings.Join(ids, ","))...)
	if err == nil && len(latest) == 0 {
		fail(w, 404, errNothingToExport.Error())
		return
	}
	if err == nil {
		err = o.auditExport(ctx, actor, latest)
	}
	if err != nil {
		o.unavailable(w, err)
		return
	}
	// Hundreds of configurations take longer to send than the server's usual write timeout.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Minute))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", attachment("configs-"+time.Now().In(o.loc).Format("20060102-1504")+".zip"))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	archive := zip.NewWriter(w)
	for _, b := range latest {
		if err = o.writeConfigEntry(ctx, archive, b); err != nil {
			// The status is already sent; a cut-short archive fails to open rather than looking whole.
			log.Printf("configuration export stopped at switch %s: %v", b.switchID, err)
			return
		}
	}
	if err = archive.Close(); err != nil {
		log.Printf("configuration export: %v", err)
	}
}

func (o *OpsHandler) latestBackups(ctx context.Context, args ...any) ([]latestBackup, error) {
	rows, err := o.db.QueryContext(ctx, `SELECT DISTINCT ON (b.switch_id) b.id,s.id::text,s.name,host(s.management_ip),b.captured_at
 FROM bp_config_backups b JOIN bp_switches s ON s.id=b.switch_id
 WHERE `+switchFilter+` AND ($10='' OR s.id=ANY(string_to_array($10,',')::uuid[]))
 ORDER BY b.switch_id,b.captured_at DESC,b.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []latestBackup
	for rows.Next() {
		var b latestBackup
		if err = rows.Scan(&b.id, &b.switchID, &b.name, &b.ip, &b.capturedAt); err != nil {
			return nil, err
		}
		found = append(found, b)
	}
	return found, rows.Err()
}

func (o *OpsHandler) auditExport(ctx context.Context, actor domain.Principal, latest []latestBackup) error {
	versions := make([]string, len(latest))
	for i, b := range latest {
		versions[i] = strconv.FormatInt(b.id, 10)
	}
	_, err := o.db.ExecContext(ctx, `INSERT INTO bp_asset_events(actor_id,actor_email,actor_name,action,object_id,object_name)
 SELECT $1,$2,$3,'config.export',s.id,s.name||' (version '||b.id||')'
 FROM bp_config_backups b JOIN bp_switches s ON s.id=b.switch_id WHERE b.id=ANY(string_to_array($4,',')::bigint[])`,
		actor.User.ID, actor.User.Email, actor.User.Name, strings.Join(versions, ","))
	return err
}

func (o *OpsHandler) writeConfigEntry(ctx context.Context, archive *zip.Writer, b latestBackup) error {
	var sealed string
	err := o.db.QueryRowContext(ctx, `SELECT encrypted_content FROM bp_config_backups WHERE id=$1`, b.id).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // pruned since the list was read; nothing to send for it
	}
	if err != nil {
		return err
	}
	content, err := openConfig(o.key, sealed)
	if err != nil {
		return err
	}
	entry, err := archive.CreateHeader(&zip.FileHeader{
		Name:     configFileName(b.name, b.ip, b.capturedAt, o.loc),
		Method:   zip.Deflate,
		Modified: b.capturedAt.In(o.loc),
	})
	if err != nil {
		return err
	}
	_, err = entry.Write([]byte(content))
	return err
}
