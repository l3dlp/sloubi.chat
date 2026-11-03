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
	DB *sql.DB
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
	);`); err != nil {
		return err
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
	INSERT OR IGNORE INTO channels (name, created_at, last_message_at)
		SELECT channel, MIN(ts), MAX(ts) FROM messages GROUP BY channel;`)
	return err
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

	// valide seulement "sloubi N" avec N = dernier + 1, et pas deux fois de suite par le même auteur
	if matches := sloubiExactRe.FindStringSubmatch(m.Message); len(matches) == 2 {
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
	return m, created, tx.Commit()
}

func (s *Store) lastMessages(channel string, limit int) ([]chat.Message, error) {
	rows, err := s.DB.Query(`SELECT id, channel, client_id, nickname, message, ts, sloubi_number
		FROM messages WHERE channel = ? ORDER BY id DESC LIMIT ?`, channel, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rev []chat.Message
	for rows.Next() {
		var m chat.Message
		var n sql.NullInt64
		if err := rows.Scan(&m.ID, &m.Channel, &m.ClientID, &m.Nickname, &m.Message, &m.TimestampISO, &n); err != nil {
			return nil, err
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
