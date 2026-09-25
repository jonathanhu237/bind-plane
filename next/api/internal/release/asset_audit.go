package release

import (
	"net/http"
	"time"
)

func (a *AdminHandler) audit(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authorize(w, r, false); !ok {
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,actor_email,action,object_id::text,object_name,created_at FROM bp_asset_events ORDER BY id DESC LIMIT 200`)
	if err != nil {
		fail(w, 503, "asset audit unavailable")
		return
	}
	defer rows.Close()
	events := []map[string]any{}
	for rows.Next() {
		var id int64
		var actor, action, objectID, name string
		var created time.Time
		if err = rows.Scan(&id, &actor, &action, &objectID, &name, &created); err != nil {
			fail(w, 503, "asset audit unavailable")
			return
		}
		events = append(events, map[string]any{"id": id, "actor_email": actor, "action": action, "object_id": objectID, "object_name": name, "created_at": created})
	}
	if err = rows.Err(); err != nil {
		fail(w, 503, "asset audit unavailable")
		return
	}
	write(w, 200, events)
}
