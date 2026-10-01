package notifications

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"time"

	"github.com/godbus/dbus/v5"
	_ "modernc.org/sqlite" // the pure-Go sqlite driver: notifications.db stays the Rust file
)

// imageDataKeys are the inline image hints the store leaves out
// (IMAGE_DATA_KEYS): the pixels are cached as a file, whose path the
// image_path column keeps.
var imageDataKeys = []string{"image-data", "image_data", "icon_data"}

// Store is the notification history on disk (persistence.rs): the same
// sqlite file, table, and column encodings the Rust shell writes, so
// the history survives a restart and a switch between the two.
type Store struct {
	db *sql.DB
}

// StorePath is persistence.rs's path: $HOME/.local/share/wayle/
// notifications.db (HOME, not XDG_DATA_HOME, as the Rust store does).
func StorePath() (string, error) {
	home := os.Getenv("HOME")
	if home == "" {
		return "", errors.New("notifications: HOME is not set")
	}
	return filepath.Join(home, ".local", "share", "wayle", "notifications.db"), nil
}

// OpenStore opens (creating if needed) the history at path; tests pass
// a temporary file.
func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // the shared data dir, as the Rust store creates it
		return nil, fmt.Errorf("notification store: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("notification store: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS notifications (
			id INTEGER PRIMARY KEY,
			app_name TEXT,
			replaces_id INTEGER,
			app_icon TEXT,
			summary TEXT NOT NULL,
			body TEXT,
			actions TEXT NOT NULL,
			hints TEXT NOT NULL,
			expire_timeout INTEGER,
			timestamp INTEGER NOT NULL,
			image_path TEXT
		);
		PRAGMA journal_mode = WAL;
		PRAGMA synchronous = NORMAL;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("notification store: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database.
func (st *Store) Close() error { return st.db.Close() }

// Add stores or replaces one notification (INSERT OR REPLACE by id).
func (st *Store) Add(n *Notification) error {
	actions := make([]string, 0, 2*len(n.Actions))
	for _, a := range n.Actions {
		actions = append(actions, a.ID, a.Label)
	}
	actionsJSON, err := json.Marshal(actions)
	if err != nil {
		return err
	}
	hintsJSON, err := encodeHints(n.Hints)
	if err != nil {
		return err
	}
	_, err = st.db.Exec(`INSERT OR REPLACE INTO notifications
		(id, app_name, replaces_id, app_icon, summary, body, actions, hints,
		 expire_timeout, timestamp, image_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, nullString(n.AppName), nullID(n.ReplacesID), nullString(n.AppIcon), n.Summary,
		nullString(n.Body), string(actionsJSON), hintsJSON, nullTimeout(n.ExpireMS),
		n.Added.UnixMilli(), nullString(n.ImagePath))
	return err
}

// Remove deletes one notification.
func (st *Store) Remove(id uint32) error {
	_, err := st.db.Exec(`DELETE FROM notifications WHERE id = ?`, id)
	return err
}

// Load reads the history newest first (load_all). With removeExpired,
// a notification whose positive timeout ran out while wayle was not
// running stays out.
func (st *Store) Load(now time.Time, removeExpired bool) ([]*Notification, error) {
	rows, err := st.db.Query(`SELECT id, app_name, replaces_id, app_icon, summary, body,
		actions, hints, expire_timeout, timestamp, image_path
		FROM notifications ORDER BY timestamp DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*Notification
	for rows.Next() {
		var (
			id                                uint32
			appName, appIcon, body, imagePath sql.NullString
			replaces, timeout                 sql.NullInt64
			summary, actionsJSON, hintsJSON   string
			stamp                             int64
		)
		if err := rows.Scan(&id, &appName, &replaces, &appIcon, &summary, &body,
			&actionsJSON, &hintsJSON, &timeout, &stamp, &imagePath); err != nil {
			return nil, err
		}
		var actions []string
		if err := json.Unmarshal([]byte(actionsJSON), &actions); err != nil {
			log.Printf("notifications: stored actions of %d: %v", id, err)
		}
		raw := decodeStoredHints(id, hintsJSON)
		if imagePath.Valid {
			raw["image-path"] = dbus.MakeVariant(imagePath.String)
		}
		h := decodeHints(raw, func(imageData) (string, bool) { return "", false })
		expire := int32(-1)
		if timeout.Valid {
			expire = int32(timeout.Int64)
		}
		n := &Notification{
			ID:           id,
			AppName:      appName.String,
			ReplacesID:   uint32(replaces.Int64),
			AppIcon:      appIcon.String,
			Summary:      summary,
			Body:         body.String,
			Actions:      ParseActions(actions),
			ExpireMS:     expire,
			Added:        time.UnixMilli(stamp),
			Urgency:      h.urgency,
			ImagePath:    h.imagePath,
			DesktopEntry: h.desktopEntry,
			Resident:     h.resident,
			Hints:        raw,
		}
		if expire > 0 {
			n.Expires = n.Added.Add(time.Duration(expire) * time.Millisecond)
			if removeExpired && !n.Expires.After(now) {
				continue
			}
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// storedHints are the hints a notification keeps for the store: all of
// them but the inline image pixels.
func storedHints(raw map[string]dbus.Variant) map[string]dbus.Variant {
	out := maps.Clone(raw)
	for _, key := range imageDataKeys {
		delete(out, key)
	}
	return out
}

// storedHint is zvariant's serde form of a value, which the Rust store
// writes: {"signature": "y", "value": 2}.
type storedHint struct {
	Signature string          `json:"signature"`
	Value     json.RawMessage `json:"value"`
}

// encodeHints writes the hints as the Rust store does. Only basic-type
// values have one JSON form to write; a container hint (rare, and read
// by nothing here) is left out rather than written in a shape the Rust
// side would not parse.
func encodeHints(raw map[string]dbus.Variant) (string, error) {
	out := make(map[string]storedHint, len(raw))
	for key, v := range raw {
		sig := v.Signature().String()
		if !basicSignature(sig) {
			continue
		}
		value, err := json.Marshal(v.Value())
		if err != nil {
			return "", err
		}
		out[key] = storedHint{Signature: sig, Value: value}
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// decodeStoredHints reads the hints column back into variants,
// dropping what it cannot read (the Rust store warns and keeps none of
// a broken map, one broken value).
func decodeStoredHints(id uint32, text string) map[string]dbus.Variant {
	out := map[string]dbus.Variant{}
	var stored map[string]storedHint
	if err := json.Unmarshal([]byte(text), &stored); err != nil {
		log.Printf("notifications: stored hints of %d: %v", id, err)
		return out
	}
	for key, h := range stored {
		if v, ok := basicVariant(h); ok {
			out[key] = v
		}
	}
	return out
}

// basicSignature reports a single basic D-Bus type.
func basicSignature(sig string) bool {
	switch sig {
	case "y", "b", "n", "q", "i", "u", "x", "t", "d", "s", "o", "g":
		return true
	}
	return false
}

// basicVariant rebuilds a basic-type variant from its stored form.
func basicVariant(h storedHint) (dbus.Variant, bool) {
	dec := json.NewDecoder(bytes.NewReader(h.Value))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return dbus.Variant{}, false
	}
	if s, ok := v.(string); ok {
		switch h.Signature {
		case "s":
			return dbus.MakeVariant(s), true
		case "o":
			return dbus.MakeVariant(dbus.ObjectPath(s)), true
		case "g":
			sig, err := dbus.ParseSignature(s)
			return dbus.MakeVariant(sig), err == nil
		}
		return dbus.Variant{}, false
	}
	if b, ok := v.(bool); ok {
		return dbus.MakeVariant(b), h.Signature == "b"
	}
	num, ok := v.(json.Number)
	if !ok {
		return dbus.Variant{}, false
	}
	if h.Signature == "d" {
		f, err := num.Float64()
		return dbus.MakeVariant(f), err == nil
	}
	i, err := num.Int64()
	if err != nil {
		if h.Signature == "t" {
			var u uint64
			_, err := fmt.Sscan(num.String(), &u)
			return dbus.MakeVariant(u), err == nil
		}
		return dbus.Variant{}, false
	}
	switch h.Signature {
	case "y":
		return dbus.MakeVariant(byte(i)), i >= 0 && i <= 0xff
	case "n":
		return dbus.MakeVariant(int16(i)), i >= -1<<15 && i < 1<<15
	case "q":
		return dbus.MakeVariant(uint16(i)), i >= 0 && i < 1<<16
	case "i":
		return dbus.MakeVariant(int32(i)), i >= -1<<31 && i < 1<<31
	case "u":
		return dbus.MakeVariant(uint32(i)), i >= 0 && i < 1<<32
	case "x":
		return dbus.MakeVariant(i), true
	case "t":
		return dbus.MakeVariant(uint64(i)), i >= 0
	}
	return dbus.Variant{}, false
}

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func nullID(id uint32) sql.NullInt64 { return sql.NullInt64{Int64: int64(id), Valid: id != 0} }

// nullTimeout is the Rust Option<u32>: a negative (server default)
// timeout is none.
func nullTimeout(ms int32) sql.NullInt64 { return sql.NullInt64{Int64: int64(ms), Valid: ms >= 0} }
