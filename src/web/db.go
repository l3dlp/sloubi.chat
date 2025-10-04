package web

import (
	"database/sql"
	"log"
	"os"
	"regexp"
	"sloubi/src/chat"
	"strconv"
	"time"

	_ "modernc.org/sqlite" // <-- indispensable : charge le driver SQLite pur Go
)

type Store struct {
	DB *sql.DB
}

func ensureDirs() {
	_ = os.MkdirAll("db", 0o755)
	_ = os.MkdirAll("views", 0o755)
	_ = os.MkdirAll("assets", 0o755)
}

func openStore() *Store {
	ensureDirs()
	db, err := sql.Open("sqlite", "db/chat.sqlite?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		log.Fatal(err)
	}
	schema := `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		client_id TEXT,
		nickname TEXT,
		message TEXT,
		ts TEXT,
		sloubi_number INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_messages_ts ON messages(ts);
	CREATE INDEX IF NOT EXISTS idx_messages_sloubi ON messages(sloubi_number);
	`
	if _, err := db.Exec(schema); err != nil {
		log.Fatal(err)
	}
	return &Store{DB: db}
}

var (
	sloubiExactRe = regexp.MustCompile(`(?i)\bsloubi\s*(\d+)\b`)
)

func (s *Store) lastSloubiEntry() (num int, lastClientID string) {
	_ = s.DB.QueryRow(`SELECT COALESCE(MAX(sloubi_number),0),
		COALESCE((SELECT client_id FROM messages WHERE sloubi_number IS NOT NULL ORDER BY sloubi_number DESC LIMIT 1),'')
		FROM messages`).Scan(&num, &lastClientID)
	return
}

func (s *Store) insertMessage(m chat.Message) (chat.Message, error) {
	// par défaut : pas un sloubi validé
	var sloubi *int

	// timestamp
	ts := m.TimestampISO
	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339)
	}

	// check si message contient "sloubi N"
	matches := sloubiExactRe.FindStringSubmatch(m.Message)
	if len(matches) == 2 {
		claimed, _ := strconv.Atoi(matches[1])

		// récupérer dernier sloubi + client associé
		current, lastClient := s.lastSloubiEntry()
		expected := current + 1

		// conditions :
		// - nombre exact attendu
		// - pas le même auteur que le dernier validé
		if claimed == expected && m.ClientID != lastClient {
			sloubi = &expected
		}
	}

	res, err := s.DB.Exec(`INSERT INTO messages (client_id, nickname, message, ts, sloubi_number)
		VALUES (?,?,?,?,?)`, m.ClientID, m.Nickname, m.Message, ts, sloubi)
	if err != nil {
		return m, err
	}
	id, _ := res.LastInsertId()

	m.ID = id
	m.TimestampISO = ts
	m.SloubiNumber = sloubi
	return m, nil
}

func (s *Store) lastMessages(limit int) ([]chat.Message, error) {
	rows, err := s.DB.Query(`SELECT id, client_id, nickname, message, ts, sloubi_number
		FROM messages ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rev []chat.Message
	for rows.Next() {
		var m chat.Message
		var n sql.NullInt64
		if err := rows.Scan(&m.ID, &m.ClientID, &m.Nickname, &m.Message, &m.TimestampISO, &n); err != nil {
			return nil, err
		}
		if n.Valid {
			v := int(n.Int64)
			m.SloubiNumber = &v
		}
		rev = append(rev, m)
	}
	// reverse to chronological asc for playback
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, nil
}

func (s *Store) lastSloubi() (m *chat.Message, err error) {
	row := s.DB.QueryRow(`SELECT id, client_id, nickname, message, ts, sloubi_number
		FROM messages WHERE sloubi_number IS NOT NULL ORDER BY sloubi_number DESC LIMIT 1`)
	var r chat.Message
	var n sql.NullInt64
	switch err = row.Scan(&r.ID, &r.ClientID, &r.Nickname, &r.Message, &r.TimestampISO, &n); err {
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
