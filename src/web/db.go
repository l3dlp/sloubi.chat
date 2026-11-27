package web

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sloubi/src/chat"
	"strconv"
	"sync"
	"time"

	_ "modernc.org/sqlite" // <-- indispensable : charge le driver SQLite pur Go
)

// DefaultChannel accueille l'ancienne taverne unique (ex-"/").
const DefaultChannel = "chante"

const tsLayout = "2006-01-02T15:04:05.000Z"

type Store struct {
	DB     *sql.DB
	secret []byte // clé HMAC des cookies d'accès, tirée une fois et gardée en base
	// wmu sérialise les écritures : la vérification "N = max + 1" et l'INSERT
	// doivent être atomiques, sinon deux "sloubi N" simultanés passent tous les deux.
	wmu sync.Mutex
}

func now() string { return time.Now().UTC().Format(tsLayout) }

func openStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migration: %w", err)
	}
	if s.secret, err = s.loadSecret(); err != nil {
		db.Close()
		return nil, fmt.Errorf("secret: %w", err)
	}
	// reporte le WAL dans le fichier principal : une copie de chat.sqlite seul reste
	// une sauvegarde valable, et le WAL ne grossit pas sans fin
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		log.Printf("checkpoint: %v", err)
	}
	go func() {
		for range time.Tick(time.Hour) {
			_, _ = db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
		}
	}()
	return s, nil
}

func (s *Store) hasColumn(table, col string) (bool, error) {
	rows, err := s.DB.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) migrate() error {
	if _, err := s.DB.Exec(`
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		client_id TEXT,
		nickname TEXT,
		message TEXT,
		ts TEXT,
		sloubi_number INTEGER
	);
	CREATE TABLE IF NOT EXISTS channels (
		name TEXT PRIMARY KEY,
		created_at TEXT NOT NULL,
		last_message_at TEXT
	);
	CREATE TABLE IF NOT EXISTS postits (
		channel TEXT PRIMARY KEY,
		text TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		updated_by TEXT
	);
	CREATE TABLE IF NOT EXISTS meta (
		key TEXT PRIMARY KEY,
		value BLOB NOT NULL
	);
	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		channel TEXT NOT NULL,
		name TEXT NOT NULL,
		mime TEXT NOT NULL,
		kind TEXT NOT NULL,
		size INTEGER NOT NULL,
		w INTEGER,
		h INTEGER,
		thumb INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_files_channel_kind ON files(channel, kind);
	CREATE TABLE IF NOT EXISTS video_meta (
		provider TEXT NOT NULL,
		vid TEXT NOT NULL,
		title TEXT,
		author TEXT,
		thumb INTEGER NOT NULL DEFAULT 0,
		fetched_at TEXT NOT NULL,
		PRIMARY KEY (provider, vid)
	);
	CREATE TABLE IF NOT EXISTS likes (
		message_id INTEGER PRIMARY KEY,
		n INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS link_clicks (
		message_id INTEGER NOT NULL,
		idx INTEGER NOT NULL,
		n INTEGER NOT NULL,
		PRIMARY KEY (message_id, idx)
	);
	CREATE TABLE IF NOT EXISTS push_subs (
		endpoint TEXT NOT NULL,
		channel TEXT NOT NULL,
		p256dh TEXT NOT NULL,
		auth TEXT NOT NULL,
		tag TEXT,
		created_at TEXT NOT NULL,
		PRIMARY KEY (endpoint, channel)
	);
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT NOT NULL UNIQUE,
		role TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at TEXT NOT NULL,
		requested_at TEXT
	);
	CREATE TABLE IF NOT EXISTS otp_codes (
		email TEXT PRIMARY KEY,
		code_hash TEXT NOT NULL,
		issued_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		attempts INTEGER NOT NULL DEFAULT 0
	);`); err != nil {
		return err
	}

	// v4 : un message peut porter un fichier
	if ok, err := s.hasColumn("messages", "file_id"); err != nil {
		return err
	} else if !ok {
		if _, err := s.DB.Exec(`ALTER TABLE messages ADD COLUMN file_id TEXT`); err != nil {
			return err
		}
	}

	// v3 : thème, créateur et mot de passe de chaque lieu
	// v5 : le compte propriétaire, et le mot de passe qui attend la validation de l'admin
	for col, def := range map[string]string{
		"theme":      `TEXT NOT NULL DEFAULT '` + DefaultTheme + `'`,
		"owner_hash": "TEXT",
		"pw_hash":    "TEXT",
		"owner_user": "INTEGER",
		"pw_pending": "TEXT",
		"domain":     "TEXT", // v6 : le domaine perso du lieu (domains.go)
	} {
		ok, err := s.hasColumn("channels", col)
		if err != nil {
			return err
		}
		if !ok {
			if _, err := s.DB.Exec(`ALTER TABLE channels ADD COLUMN ` + col + ` ` + def); err != nil {
				return err
			}
		}
	}

	// v1 → v2 : les messages existants appartiennent à la taverne historique.
	ok, err := s.hasColumn("messages", "channel")
	if err != nil {
		return err
	}
	if !ok {
		if _, err := s.DB.Exec(`ALTER TABLE messages ADD COLUMN channel TEXT NOT NULL DEFAULT '` + DefaultChannel + `'`); err != nil {
			return err
		}
	}

	_, err = s.DB.Exec(`
	DROP INDEX IF EXISTS idx_messages_sloubi;
	CREATE INDEX IF NOT EXISTS idx_messages_ts ON messages(ts);
	CREATE INDEX IF NOT EXISTS idx_messages_channel_id ON messages(channel, id);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_channel_sloubi ON messages(channel, sloubi_number);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_domain ON channels(domain);
	INSERT OR IGNORE INTO channels (name, created_at, last_message_at)
		SELECT channel, MIN(ts), MAX(ts) FROM messages GROUP BY channel;`)
	if err != nil {
		return err
	}
	// les #mots de la forêt ; à la création, on reprend les messages déjà écrits
	var hasTags int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'tags'`).Scan(&hasTags); err != nil {
		return err
	}
	if hasTags == 0 {
		if err := s.createTags(); err != nil {
			return err
		}
	}
	return nil
}

var sloubiExactRe = regexp.MustCompile(`(?i)\bsloubi\s*(\d+)\b`)

func (s *Store) lastSloubiEntry(q interface {
	QueryRow(string, ...any) *sql.Row
}, channel string) (num int, lastClientID string, err error) {
	err = q.QueryRow(`SELECT COALESCE(MAX(sloubi_number),0),
		COALESCE((SELECT client_id FROM messages WHERE channel = ?1 AND sloubi_number IS NOT NULL ORDER BY sloubi_number DESC LIMIT 1),'')
		FROM messages WHERE channel = ?1`, channel).Scan(&num, &lastClientID)
	return
}

func (s *Store) channelExists(channel string) (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM channels WHERE name = ?`, channel).Scan(&n)
	return n > 0, err
}

// insertMessage enregistre un message et décide côté serveur s'il valide un sloubi.
// created indique que ce message a fondé le salon.
func (s *Store) insertMessage(m chat.Message) (out chat.Message, created bool, err error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	tx, err := s.DB.Begin()
	if err != nil {
		return m, false, err
	}
	defer tx.Rollback()

	m.TimestampISO = now()
	m.SloubiNumber = nil

	theme := DefaultTheme
	if err := tx.QueryRow(`SELECT theme FROM channels WHERE name = ?`, m.Channel).Scan(&theme); err != nil && err != sql.ErrNoRows {
		return m, false, err
	}

	// valide seulement "sloubi N" avec N = dernier + 1, et pas deux fois de suite par le même auteur.
	// On ne compte les sloubis qu'à la taverne.
	if matches := sloubiExactRe.FindStringSubmatch(m.Message); len(matches) == 2 && theme == DefaultTheme {
		claimed, _ := strconv.Atoi(matches[1])
		current, lastClient, err := s.lastSloubiEntry(tx, m.Channel)
		if err != nil {
			return m, false, err
		}
		if expected := current + 1; claimed == expected && m.ClientID != lastClient {
			m.SloubiNumber = &expected
		}
	}

	res, err := tx.Exec(`INSERT OR IGNORE INTO channels (name, created_at) VALUES (?, ?)`, m.Channel, m.TimestampISO)
	if err != nil {
		return m, false, err
	}
	n, _ := res.RowsAffected()
	created = n > 0
	if _, err := tx.Exec(`UPDATE channels SET last_message_at = ? WHERE name = ?`, m.TimestampISO, m.Channel); err != nil {
		return m, false, err
	}

	res, err = tx.Exec(`INSERT INTO messages (channel, client_id, nickname, message, ts, sloubi_number)
		VALUES (?,?,?,?,?,?)`, m.Channel, m.ClientID, m.Nickname, m.Message, m.TimestampISO, m.SloubiNumber)
	if err != nil {
		return m, false, err
	}
	if m.ID, err = res.LastInsertId(); err != nil {
		return m, false, err
	}
	for _, tag := range extractTags(m.Message) {
		if _, err := tx.Exec(`INSERT INTO tags (channel, tag, message_id) VALUES (?,?,?)`, m.Channel, tag, m.ID); err != nil {
			return m, false, err
		}
	}
	return m, created, tx.Commit()
}

func (s *Store) lastMessages(channel string, limit int) ([]chat.Message, error) {
	rows, err := s.DB.Query(`SELECT m.id, m.channel, m.client_id, m.nickname, m.message, m.ts, m.sloubi_number, COALESCE(l.n, 0),
			f.id, f.name, f.mime, f.kind, f.size, f.w, f.h, f.thumb
		FROM messages m LEFT JOIN likes l ON l.message_id = m.id LEFT JOIN files f ON f.id = m.file_id
		WHERE m.channel = ? ORDER BY m.id DESC LIMIT ?`, channel, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rev []chat.Message
	for rows.Next() {
		var m chat.Message
		var n sql.NullInt64
		var fid, fname, fmime, fkind sql.NullString
		var fsize, fw, fh sql.NullInt64
		var fthumb sql.NullBool
		if err := rows.Scan(&m.ID, &m.Channel, &m.ClientID, &m.Nickname, &m.Message, &m.TimestampISO, &n, &m.Likes,
			&fid, &fname, &fmime, &fkind, &fsize, &fw, &fh, &fthumb); err != nil {
			return nil, err
		}
		if fid.Valid {
			m.File = &chat.FileRef{ID: fid.String, Name: fname.String, Mime: fmime.String, Kind: fkind.String,
				Size: fsize.Int64, W: int(fw.Int64), H: int(fh.Int64), Thumb: fthumb.Bool}
		}
		if n.Valid {
			v := int(n.Int64)
			m.SloubiNumber = &v
		}
		rev = append(rev, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// reverse to chronological asc for playback
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	if err := s.fillClicks(channel, rev); err != nil {
		return nil, err
	}
	return rev, nil
}

func (s *Store) lastSloubi(channel string) (*chat.Message, error) {
	row := s.DB.QueryRow(`SELECT id, channel, client_id, nickname, message, ts, sloubi_number
		FROM messages WHERE channel = ? AND sloubi_number IS NOT NULL ORDER BY sloubi_number DESC LIMIT 1`, channel)
	var r chat.Message
	var n sql.NullInt64
	switch err := row.Scan(&r.ID, &r.Channel, &r.ClientID, &r.Nickname, &r.Message, &r.TimestampISO, &n); err {
	case sql.ErrNoRows:
		return nil, nil
	case nil:
		v := int(n.Int64)
		r.SloubiNumber = &v
		return &r, nil
	default:
		return nil, err
	}
}

// ChannelInfo : une ligne de l'annuaire /_/.
type ChannelInfo struct {
	Name       string
	Theme      string
	Locked     bool
	CreatedAt  string
	LastActive string
	Messages   int
	Sloubis    int
}

// listChannels renvoie les salons, les plus récemment actifs d'abord.
func (s *Store) listChannels(limit int) ([]ChannelInfo, error) {
	rows, err := s.DB.Query(`SELECT c.name, c.theme, c.pw_hash IS NOT NULL, c.created_at, COALESCE(c.last_message_at, c.created_at),
			COUNT(m.id), COALESCE(MAX(m.sloubi_number), 0)
		FROM channels c LEFT JOIN messages m ON m.channel = c.name
		GROUP BY c.name ORDER BY 5 DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChannelInfo
	for rows.Next() {
		var ci ChannelInfo
		if err := rows.Scan(&ci.Name, &ci.Theme, &ci.Locked, &ci.CreatedAt, &ci.LastActive, &ci.Messages, &ci.Sloubis); err != nil {
			return nil, err
		}
		out = append(out, ci)
	}
	return out, rows.Err()
}

func (s *Store) postit(channel string) (chat.Postit, error) {
	p := chat.Postit{Channel: channel}
	err := s.DB.QueryRow(`SELECT text, updated_at FROM postits WHERE channel = ?`, channel).Scan(&p.Text, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return p, nil
	}
	return p, err
}

// setPostit remplace le post-it du salon (le dernier qui écrit gagne) et fonde le salon au besoin.
func (s *Store) setPostit(channel, text, clientID string) (p chat.Postit, created bool, err error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	tx, err := s.DB.Begin()
	if err != nil {
		return p, false, err
	}
	defer tx.Rollback()

	ts := now()
	res, err := tx.Exec(`INSERT OR IGNORE INTO channels (name, created_at) VALUES (?, ?)`, channel, ts)
	if err != nil {
		return p, false, err
	}
	n, _ := res.RowsAffected()
	if _, err := tx.Exec(`INSERT INTO postits (channel, text, updated_at, updated_by) VALUES (?1, ?2, ?3, ?4)
		ON CONFLICT(channel) DO UPDATE SET text = excluded.text, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		channel, text, ts, clientID); err != nil {
		return p, false, err
	}
	return chat.Postit{Channel: channel, Text: text, UpdatedAt: ts}, n > 0, tx.Commit()
}
