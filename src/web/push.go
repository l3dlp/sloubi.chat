package web

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

/* Web Push : prévenir les membres d'un lieu gardé qu'un appel commence, même
   onglet fermé. Écrit avec la bibliothèque standard : chiffrement du contenu
   (RFC 8291, aes128gcm) et signature VAPID (RFC 8292, ES256). Les services de
   push (Google, Mozilla, Apple, Microsoft) ne voient que du chiffré.

   Les présents dans le salon (WebSocket ouvert) ne reçoivent pas de push : leur
   page sonne elle-même et affiche la notification si l'onglet est caché. */

const (
	maxPushPerRoom = 300
	pushEvery      = 90 * time.Second // au plus un push d'appel par salon et par période
	pushTTL        = 120              // secondes : un appel d'il y a 10 minutes n'intéresse plus
)

// seuls les vrais services de push : le serveur ne poste pas vers une URL arbitraire (SSRF)
var pushHosts = []string{
	"fcm.googleapis.com", "android.googleapis.com",
	".push.services.mozilla.com",
	".notify.windows.com",
	"web.push.apple.com", ".push.apple.com",
}

func pushHostAllowed(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, ok := range pushHosts {
		if h == ok || (strings.HasPrefix(ok, ".") && strings.HasSuffix(h, ok)) {
			return true
		}
	}
	return false
}

var pushTagRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

/* ========== Clés VAPID, tirées une fois et gardées en base ========== */

func (s *Store) loadVAPID() (*ecdsa.PrivateKey, error) {
	var b []byte
	err := s.DB.QueryRow(`SELECT value FROM meta WHERE key = 'vapid_private'`).Scan(&b)
	if err == nil {
		return ecdsa.ParseRawPrivateKey(elliptic.P256(), b)
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	raw, err := k.Bytes()
	if err != nil {
		return nil, err
	}
	if _, err := s.DB.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('vapid_private', ?)`, raw); err != nil {
		return nil, err
	}
	return k, nil
}

/* ========== Abonnements ========== */

type pushSub struct {
	Endpoint string
	P256dh   string // clé publique du navigateur, base64url
	Auth     string // secret d'authentification, base64url
	Tag      string // identifiant du navigateur, pour ne pas prévenir qui est déjà là
}

func (s *Store) addPushSub(channel string, p pushSub) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM push_subs WHERE channel = ?`, channel).Scan(&n); err != nil {
		return err
	}
	if n >= maxPushPerRoom {
		return errors.New("too many subscriptions")
	}
	_, err := s.DB.Exec(`INSERT INTO push_subs (endpoint, channel, p256dh, auth, tag, created_at) VALUES (?,?,?,?,?,?)
		ON CONFLICT(endpoint, channel) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth, tag = excluded.tag`,
		p.Endpoint, channel, p.P256dh, p.Auth, p.Tag, now())
	return err
}

func (s *Store) removePushSub(channel, endpoint string) error {
	_, err := s.DB.Exec(`DELETE FROM push_subs WHERE channel = ? AND endpoint = ?`, channel, endpoint)
	return err
}

// dropEndpoint : le service dit que l'abonnement n'existe plus (404/410), dans aucun salon.
func (s *Store) dropEndpoint(endpoint string) {
	_, _ = s.DB.Exec(`DELETE FROM push_subs WHERE endpoint = ?`, endpoint)
}

func (s *Store) pushSubs(channel string) ([]pushSub, error) {
	rows, err := s.DB.Query(`SELECT endpoint, p256dh, auth, tag FROM push_subs WHERE channel = ?`, channel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pushSub
	for rows.Next() {
		var p pushSub
		if err := rows.Scan(&p.Endpoint, &p.P256dh, &p.Auth, &p.Tag); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

/* ========== Chiffrement (RFC 8291) ========== */

func b64(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// encryptPush chiffre payload pour un navigateur (clé publique uaPub, secret auth).
func encryptPush(payload, uaPub, auth []byte) ([]byte, error) {
	curve := ecdh.P256()
	ua, err := curve.NewPublicKey(uaPub)
	if err != nil {
		return nil, fmt.Errorf("p256dh: %w", err)
	}
	if len(auth) != 16 {
		return nil, errors.New("auth: 16 octets attendus")
	}
	as, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := as.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPub := as.PublicKey().Bytes()

	prkKey, err := hkdf.Extract(sha256.New, shared, auth)
	if err != nil {
		return nil, err
	}
	keyInfo := "WebPush: info\x00" + string(uaPub) + string(asPub)
	ikm, err := hkdf.Expand(sha256.New, prkKey, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// un seul enregistrement : le contenu, puis le délimiteur de dernier enregistrement
	ct := gcm.Seal(nil, nonce, append(append([]byte{}, payload...), 0x02), nil)

	var body bytes.Buffer
	body.Write(salt)
	binary.Write(&body, binary.BigEndian, uint32(4096))
	body.WriteByte(byte(len(asPub)))
	body.Write(asPub)
	body.Write(ct)
	return body.Bytes(), nil
}

/* ========== Signature VAPID (RFC 8292) ========== */

func vapidAuth(key *ecdsa.PrivateKey, endpoint, subject string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{
		"aud": u.Scheme + "://" + u.Host,
		"exp": time.Now().Add(12 * time.Hour).Unix(),
		"sub": subject,
	})
	unsigned := header + "." + enc.EncodeToString(claims)
	h := sha256.Sum256([]byte(unsigned))
	r, sg, err := ecdsa.Sign(rand.Reader, key, h[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64) // ES256 : r et s bruts, 32 octets chacun
	r.FillBytes(sig[:32])
	sg.FillBytes(sig[32:])
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("vapid t=%s.%s, k=%s", unsigned, enc.EncodeToString(sig), enc.EncodeToString(pub)), nil
}

/* ========== Envoi ========== */

type pusher struct {
	key     *ecdsa.PrivateKey
	client  *http.Client
	allowed func(string) bool // pushHostAllowed ; remplaçable dans les tests
	subject string

	mu   sync.Mutex
	last map[string]time.Time // dernier push d'appel par salon
}

func newPusher(store *Store) *pusher {
	k, err := store.loadVAPID()
	if err != nil {
		log.Printf("push: clés VAPID : %v (notifications désactivées)", err)
		return nil
	}
	return &pusher{
		key:     k,
		client:  &http.Client{Timeout: 10 * time.Second},
		allowed: pushHostAllowed,
		subject: "https://sloubi.chat",
		last:    map[string]time.Time{},
	}
}

func (p *pusher) publicKey() string {
	b, _ := p.key.PublicKey.Bytes()
	return base64.RawURLEncoding.EncodeToString(b)
}

// send : un push vers un navigateur. gone indique un abonnement mort, à oublier.
func (p *pusher) send(sub pushSub, payload []byte, topic string) (gone bool, err error) {
	if !p.allowed(sub.Endpoint) {
		return true, errors.New("endpoint refusé")
	}
	uaPub, err1 := b64(sub.P256dh)
	auth, err2 := b64(sub.Auth)
	if err1 != nil || err2 != nil {
		return true, errors.New("clés illisibles")
	}
	body, err := encryptPush(payload, uaPub, auth)
	if err != nil {
		return true, err
	}
	authz, err := vapidAuth(p.key, sub.Endpoint, p.subject)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequest(http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return true, err
	}
	req.Header.Set("Authorization", authz)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", fmt.Sprint(pushTTL))
	req.Header.Set("Urgency", "high")
	req.Header.Set("Topic", topic) // un nouvel appel remplace l'ancien s'il n'a pas encore été livré
	resp, err := p.client.Do(req)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return true, fmt.Errorf("abonnement expiré (%d)", resp.StatusCode)
	case resp.StatusCode >= 300:
		return false, fmt.Errorf("service de push : %d", resp.StatusCode)
	}
	return false, nil
}

// notifyCall prévient les abonnés absents qu'un appel commence dans le salon.
func (s *server) notifyCall(channel, nick string, present map[string]bool) {
	p := s.push
	if p == nil {
		return
	}
	p.mu.Lock()
	if time.Since(p.last[channel]) < pushEvery {
		p.mu.Unlock()
		return
	}
	p.last[channel] = time.Now()
	p.mu.Unlock()

	subs, err := s.store.pushSubs(channel)
	if err != nil {
		log.Printf("push: %v", err)
		return
	}
	payload := mustJSON(gin.H{
		"kind":  "call",
		"title": "On t’appelle dans /" + channel,
		"body":  nick + " a lancé l’appel. Viens !",
		"url":   "/" + channel,
		"tag":   "call-" + channel,
	})
	topic := "call" + strings.NewReplacer(".", "", "_", "", "-", "").Replace(channel) // Topic : base64url, 32 caractères au plus
	if len(topic) > 32 {
		topic = topic[:32]
	}
	for _, sub := range subs {
		if sub.Tag != "" && present[sub.Tag] {
			continue
		}
		if gone, err := p.send(sub, payload, topic); err != nil {
			if gone {
				s.store.dropEndpoint(sub.Endpoint)
			}
			log.Printf("push %s: %v", channel, err)
		}
	}
}

/* ========== Routes ========== */

func (s *server) pushKey(c *gin.Context) {
	if s.push == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "push disabled"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": s.push.publicKey()})
}

// pushSubscribe : s'abonner (ou se désabonner) aux appels d'un lieu gardé.
func (s *server) pushSubscribe(c *gin.Context) {
	channel, ok := s.paramChannel(c)
	if !ok {
		return
	}
	if !s.checkLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	r, ok := s.enter(c, channel)
	if !ok {
		return
	}
	var in struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
		Tag    string `json:"tag"`
		Remove bool   `json:"remove"`
	}
	if !readJSON(c, 4096, &in) {
		return
	}
	if len(in.Endpoint) > 1024 || !pushHostAllowed(in.Endpoint) && (s.push == nil || !s.push.allowed(in.Endpoint)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint"})
		return
	}
	if in.Remove {
		if err := s.store.removePushSub(channel, in.Endpoint); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	if !r.Locked() {
		c.JSON(http.StatusForbidden, gin.H{"error": "calls need a password"})
		return
	}
	pub, err1 := b64(in.Keys.P256dh)
	auth, err2 := b64(in.Keys.Auth)
	if err1 != nil || err2 != nil || len(pub) != 65 || len(auth) != 16 || !pushTagRe.MatchString(in.Tag) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "keys"})
		return
	}
	if err := s.store.addPushSub(channel, pushSub{in.Endpoint, in.Keys.P256dh, in.Keys.Auth, in.Tag}); err != nil {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many subscriptions"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
