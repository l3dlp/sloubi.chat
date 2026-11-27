package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

/* ========== Comptes : connexion par code e-mail (OTP), comme Daily Jungle ==========

Discuter ne demande toujours aucun compte. Le compte sert à une seule chose :
garder un lieu par mot de passe. Seuls l'admin et les membres validés le peuvent.
Un créateur pose son mot de passe ; s'il n'est pas encore validé, c'est une
demande : le hachage attend dans channels.pw_pending (l'admin ne voit jamais le
mot de passe) et devient le vrai mot de passe quand l'admin valide le membre.

L'admin est unique : l'adresse ADMIN_EMAIL. À sa création, il devient
propriétaire de tous les lieux qui existaient (une seule fois : meta.admin_claimed).
*/

const (
	roleAdmin  = "admin"
	roleMember = "membre"

	statusNone     = "aucun"      // connecté, n'a rien demandé
	statusPending  = "en-attente" // a posé un mot de passe, attend l'admin
	statusApproved = "valide"     // peut garder ses lieux
	statusRefused  = "refuse"

	otpTTL         = 10 * time.Minute
	otpResend      = 60 * time.Second
	otpMaxAttempts = 5
	sessionCookie  = "sloubi_session"
	sessionTTL     = 90 * 24 * time.Hour
	maxEmailLen    = 254
)

var emailRe = regexp.MustCompile(`^[^@\s<>"',;]+@[^@\s<>"',;]+\.[^@\s<>"',;]+$`)

func cleanEmail(e string) (string, bool) {
	e = strings.ToLower(strings.TrimSpace(e))
	return e, len(e) <= maxEmailLen && emailRe.MatchString(e)
}

type User struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt"`
	RequestedAt string `json:"requestedAt,omitempty"`
}

func (u *User) admin() bool { return u != nil && u.Role == roleAdmin }

// mayLock : peut poser un mot de passe tout de suite (sinon, c'est une demande).
func (u *User) mayLock() bool { return u != nil && (u.Role == roleAdmin || u.Status == statusApproved) }

/* ========== Stockage ========== */

const userCols = `id, email, role, status, created_at, COALESCE(requested_at, '')`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	if err := row.Scan(&u.ID, &u.Email, &u.Role, &u.Status, &u.CreatedAt, &u.RequestedAt); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) userByID(id int64) (*User, error) {
	return scanUser(s.DB.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) userByEmail(email string) (*User, error) {
	return scanUser(s.DB.QueryRow(`SELECT `+userCols+` FROM users WHERE email = ?`, email))
}

// ensureUser : le compte de cette adresse, créé au besoin.
func (s *Store) ensureUser(email string) (*User, error) {
	if _, err := s.DB.Exec(`INSERT OR IGNORE INTO users (email, role, status, created_at) VALUES (?,?,?,?)`,
		email, roleMember, statusNone, now()); err != nil {
		return nil, err
	}
	return s.userByEmail(email)
}

// ensureAdmin : l'adresse ADMIN_EMAIL est le seul admin. La première fois, il
// reçoit tous les lieux existants.
func (s *Store) ensureAdmin(email string) error {
	if email == "" {
		return nil
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO users (email, role, status, created_at) VALUES (?,?,?,?)`,
		email, roleAdmin, statusApproved, now()); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE users SET role = CASE WHEN email = ? THEN ? ELSE ? END,
		status = CASE WHEN email = ? THEN ? ELSE status END`,
		email, roleAdmin, roleMember, email, statusApproved); err != nil {
		return err
	}
	var id int64
	if err := tx.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id); err != nil {
		return err
	}
	var claimed int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM meta WHERE key = 'admin_claimed'`).Scan(&claimed); err != nil {
		return err
	}
	if claimed == 0 {
		res, err := tx.Exec(`UPDATE channels SET owner_user = ? WHERE owner_user IS NULL`, id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if _, err := tx.Exec(`INSERT INTO meta (key, value) VALUES ('admin_claimed', ?)`, []byte(now())); err != nil {
			return err
		}
		log.Printf("admin: %s devient propriétaire des %d lieux existants", email, n)
	}
	return tx.Commit()
}

/* ----- codes ----- */

var (
	errCodeTooSoon  = errors.New("un code vient de partir, réessaie dans une minute")
	errCodeNone     = errors.New("aucun code en attente, redemande-en un")
	errCodeExpired  = errors.New("code expiré, redemande-en un")
	errCodeTooMany  = errors.New("trop d'essais, redemande un code")
	errCodeMismatch = errors.New("ce n'est pas le bon code")
	errBadAction    = errors.New("action inconnue")
)

func sixDigits() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// codeHash : la base ne garde pas le code en clair.
func (s *Store) codeHash(email, code string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("otp\x00" + email + "\x00" + code))
	return hex.EncodeToString(m.Sum(nil))
}

func (s *Store) issueCode(email string) (string, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	t := time.Now().UTC()
	var issued string
	err := s.DB.QueryRow(`SELECT issued_at FROM otp_codes WHERE email = ?`, email).Scan(&issued)
	if err == nil {
		if at, perr := time.Parse(tsLayout, issued); perr == nil && t.Sub(at) < otpResend {
			return "", errCodeTooSoon
		}
	} else if err != sql.ErrNoRows {
		return "", err
	}
	code, err := sixDigits()
	if err != nil {
		return "", err
	}
	_, err = s.DB.Exec(`INSERT OR REPLACE INTO otp_codes (email, code_hash, issued_at, expires_at, attempts) VALUES (?,?,?,?,0)`,
		email, s.codeHash(email, code), t.Format(tsLayout), t.Add(otpTTL).Format(tsLayout))
	return code, err
}

func (s *Store) checkCode(email, code string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	var hash, expires string
	var attempts int
	err := s.DB.QueryRow(`SELECT code_hash, expires_at, attempts FROM otp_codes WHERE email = ?`, email).Scan(&hash, &expires, &attempts)
	if err == sql.ErrNoRows {
		return errCodeNone
	}
	if err != nil {
		return err
	}
	drop := func() { _, _ = s.DB.Exec(`DELETE FROM otp_codes WHERE email = ?`, email) }
	if exp, perr := time.Parse(tsLayout, expires); perr != nil || time.Now().UTC().After(exp) {
		drop()
		return errCodeExpired
	}
	if attempts >= otpMaxAttempts {
		drop()
		return errCodeTooMany
	}
	if subtle.ConstantTimeCompare([]byte(hash), []byte(s.codeHash(email, code))) != 1 {
		_, _ = s.DB.Exec(`UPDATE otp_codes SET attempts = attempts + 1 WHERE email = ?`, email)
		return errCodeMismatch
	}
	drop()
	return nil
}

/* ----- lieux et demandes ----- */

type ownedRoom struct {
	Name    string `json:"name"`
	Theme   string `json:"theme"`
	Locked  bool   `json:"locked"`
	Pending bool   `json:"pending"`
}

func (s *Store) roomsOf(uid int64) ([]ownedRoom, error) {
	rows, err := s.DB.Query(`SELECT name, theme, pw_hash IS NOT NULL, pw_pending IS NOT NULL FROM channels
		WHERE owner_user = ? ORDER BY COALESCE(last_message_at, created_at) DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ownedRoom{}
	for rows.Next() {
		var r ownedRoom
		if err := rows.Scan(&r.Name, &r.Theme, &r.Locked, &r.Pending); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// requestLock : un membre pas encore validé pose un mot de passe. Il devient
// propriétaire du lieu, le hachage attend l'admin, le lieu reste ouvert d'ici là.
func (s *Store) requestLock(channel string, uid int64, pwHash string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE channels SET pw_pending = ?, owner_user = ? WHERE name = ?`, pwHash, uid, channel); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE users SET status = ?, requested_at = ? WHERE id = ? AND status != ?`,
		statusPending, now(), uid, statusApproved); err != nil {
		return err
	}
	return tx.Commit()
}

// lock : le vrai mot de passe (ou "" pour le retirer). Une demande en attente tombe.
func (s *Store) lock(channel string, uid int64, pwHash string) error {
	_, err := s.DB.Exec(`UPDATE channels SET pw_hash = NULLIF(?, ''), pw_pending = NULL,
		owner_user = COALESCE(owner_user, NULLIF(?, 0)) WHERE name = ?`, pwHash, uid, channel)
	return err
}

func (s *Store) setOwnerUser(channel string, uid int64) error {
	_, err := s.DB.Exec(`UPDATE channels SET owner_user = ? WHERE name = ? AND owner_user IS NULL`, uid, channel)
	return err
}

// decide : la réponse de l'admin. Renvoie les lieux dont la serrure change.
func (s *Store) decide(uid int64, action string) ([]string, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var names []string
	var status string
	switch action {
	case "valider":
		status = statusApproved
		rows, err := tx.Query(`SELECT name FROM channels WHERE owner_user = ? AND pw_pending IS NOT NULL`, uid)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return nil, err
			}
			names = append(names, n)
		}
		rows.Close()
		if _, err := tx.Exec(`UPDATE channels SET pw_hash = pw_pending, pw_pending = NULL
			WHERE owner_user = ? AND pw_pending IS NOT NULL`, uid); err != nil {
			return nil, err
		}
	case "refuser":
		status = statusRefused
		if _, err := tx.Exec(`UPDATE channels SET pw_pending = NULL WHERE owner_user = ?`, uid); err != nil {
			return nil, err
		}
	case "retirer":
		// les lieux déjà gardés le restent : les rouvrir publierait leurs conversations.
		// Le membre ne peut plus que retirer son mot de passe, pas en poser un autre.
		status = statusNone
	default:
		return nil, errBadAction
	}
	res, err := tx.Exec(`UPDATE users SET status = ? WHERE id = ? AND role != ?`, status, uid, roleAdmin)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, sql.ErrNoRows
	}
	return names, tx.Commit()
}

type adminUser struct {
	User
	Rooms []ownedRoom `json:"rooms"`
}

func (s *Store) listUsers() ([]adminUser, error) {
	rows, err := s.DB.Query(`SELECT ` + userCols + ` FROM users ORDER BY
		CASE status WHEN '` + statusPending + `' THEN 0 WHEN '` + statusApproved + `' THEN 1 ELSE 2 END,
		COALESCE(requested_at, created_at) DESC`)
	if err != nil {
		return nil, err
	}
	var out []adminUser
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, adminUser{User: *u})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Rooms, err = s.roomsOf(out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

/* ========== Session ========== */

func (s *server) sessionToken(uid int64, exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(uid, 10) + ":" + strconv.FormatInt(exp.Unix(), 10)))
	m := hmac.New(sha256.New, s.store.secret)
	m.Write([]byte("session\x00" + payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *server) setSession(c *gin.Context, uid int64) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: sessionCookie, Value: s.sessionToken(uid, time.Now().Add(sessionTTL)), Path: "/",
		MaxAge: int(sessionTTL.Seconds()), HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: s.secureRequest(c),
	})
}

// user : le compte connecté, ou nil. Relu en base à chaque fois : une décision
// de l'admin s'applique tout de suite.
func (s *server) user(c *gin.Context) *User {
	tok, err := c.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	payload, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return nil
	}
	m := hmac.New(sha256.New, s.store.secret)
	m.Write([]byte("session\x00" + payload))
	if !hmac.Equal([]byte(sig), []byte(base64.RawURLEncoding.EncodeToString(m.Sum(nil)))) {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil
	}
	idS, expS, ok := strings.Cut(string(raw), ":")
	id, err1 := strconv.ParseInt(idS, 10, 64)
	exp, err2 := strconv.ParseInt(expS, 10, 64)
	if !ok || err1 != nil || err2 != nil || time.Now().Unix() > exp {
		return nil
	}
	u, err := s.store.userByID(id)
	if err != nil {
		return nil
	}
	return u
}

/* ========== Handlers ========== */

// POST /api/_compte/code {email} : envoie un code. Tout le monde peut se connecter.
func (s *server) authCode(c *gin.Context) {
	if !s.codeLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Doucement : réessaie dans un instant."})
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if !readJSON(c, 1024, &in) {
		return
	}
	email, ok := cleanEmail(in.Email)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cette adresse e-mail ne semble pas valide."})
		return
	}
	code, err := s.store.issueCode(email)
	if err == errCodeTooSoon {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error(), "sent": true})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if err := s.mail.SendCode(c.Request.Context(), email, code); err != nil {
		log.Printf("mail: code pour %s : %v", email, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "L'e-mail n'est pas parti. Réessaie dans un instant."})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// POST /api/_compte/verify {email, code} : ouvre la session (et crée le compte).
func (s *server) authVerify(c *gin.Context) {
	if !s.lockLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Doucement : réessaie dans un instant."})
		return
	}
	var in struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if !readJSON(c, 1024, &in) {
		return
	}
	email, ok := cleanEmail(in.Email)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cette adresse e-mail ne semble pas valide."})
		return
	}
	switch err := s.store.checkCode(email, strings.TrimSpace(in.Code)); err {
	case nil:
	case errCodeNone, errCodeExpired, errCodeTooMany, errCodeMismatch:
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	u, err := s.store.ensureUser(email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	s.setSession(c, u.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "email": u.Email, "role": u.Role, "status": u.Status})
}

func (s *server) authLogout(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /api/_compte/me : le compte et ses lieux ({signedIn:false} sans session).
func (s *server) authMe(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	u := s.user(c)
	if u == nil {
		c.JSON(http.StatusOK, gin.H{"signedIn": false})
		return
	}
	rooms, err := s.store.roomsOf(u.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"signedIn": true, "email": u.Email, "role": u.Role, "status": u.Status, "rooms": rooms})
}

func (s *server) requireAdmin(c *gin.Context) bool {
	if !s.user(c).admin() {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return false
	}
	return true
}

// GET /api/_admin/users : les comptes, les demandes en attente d'abord.
func (s *server) adminUsers(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !s.requireAdmin(c) {
		return
	}
	users, err := s.store.listUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

// POST /api/_admin/users/:id {action: valider | refuser | retirer}
func (s *server) adminDecide(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	var in struct {
		Action string `json:"action"`
	}
	if !readJSON(c, 256, &in) {
		return
	}
	locked, err := s.store.decide(id, in.Action)
	switch {
	case err == sql.ErrNoRows:
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown user"})
		return
	case err == errBadAction:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	for _, name := range locked { // ces lieux viennent de fermer leur porte : tout le monde repasse par elle
		r, err := s.store.room(name)
		if err == nil {
			s.hub.Evict(name, mustJSON(gin.H{"type": "room", "theme": r.Theme, "locked": r.Locked()}))
		}
	}
	if len(locked) > 0 {
		s.dirMu.Lock()
		s.dirAt = time.Time{}
		s.dirMu.Unlock()
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "locked": locked})
}

// GET /_/compte : la page de connexion, du compte et, pour l'admin, des demandes.
func (s *server) accountPage(c *gin.Context) {
	c.Header("X-Robots-Tag", "noindex, nofollow")
	c.Header("Cache-Control", "no-store")
	c.HTML(http.StatusOK, "account.html", gin.H{})
}
