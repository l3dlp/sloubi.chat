package web

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sloubi/src/chat"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gophre/websocket"
)

// Les vues et forbidden.json sont lus depuis la racine du repo, comme en prod.
func TestMain(m *testing.M) {
	if err := os.Chdir("../.."); err != nil {
		panic(err)
	}
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := openStore(filepath.Join(t.TempDir(), "chat.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	return s
}

func testForbidden(t *testing.T) *Forbidden {
	t.Helper()
	f, err := loadForbidden("forbidden.json")
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestCheckChannel(t *testing.T) {
	f := testForbidden(t)
	cases := map[string]string{
		"kaamelott":             "",
		"chante":                "",
		"mon-chan":              "",
		"mon-chan.v2":           "",
		"table_ronde-2026":      "",
		"badminton-club":        "",
		"amelie":                "",
		"calcul":                "",
		"computer":              "",
		"dispute":               "",
		"violet":                "",
		"metal":                 "",
		"free-party":            "",
		"a--b":                  reasonInvalid,
		"a._b":                  reasonInvalid,
		".x":                    reasonInvalid,
		"x_":                    reasonInvalid,
		"mon chan":              reasonInvalid,
		"école":                 reasonInvalid,
		"a/b":                   reasonInvalid,
		"":                      reasonInvalid,
		strings.Repeat("a", 33): reasonTooLong,
		"4dm1n":                 reasonForbidden,
		"super.admin":           reasonForbidden,
		"g00gle-support":        reasonForbidden,
		"paypal_fr":             reasonForbidden,
		"app.js":                reasonForbidden,
		"w-s":                   reasonForbidden,
		"sl0ub1":                reasonForbidden,
		"api":                   reasonForbidden,
		"wp-admin":              reasonForbidden,
		"nike":                  reasonForbidden,
		"cocacola":              reasonForbidden,
		"coca-cola":             reasonForbidden,
		"c0ca.c0la":             reasonForbidden,
		"allianz-fr":            reasonForbidden,
		"mon-adidas":            reasonForbidden,
		"loreal":                reasonForbidden,
		"nissan":                reasonForbidden,
		"bannissant":            "",
		"education":             "",
		"niveau":                "",
		"embauchant":            "",
		"revolution":            "",
		"printemps":             "",
		"mars":                  "",
		"floreal":               "",
		"insupportable":         "",
		"philanthropique":       "",
		"deconnasse":            "",
		"connasse":              reasonForbidden,
	}
	for name, want := range cases {
		if got := f.checkChannel(name); got != want {
			t.Errorf("checkChannel(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestParseForbiddenRejectsEmpty(t *testing.T) {
	if _, err := parseForbidden([]byte(`{}`)); err == nil {
		t.Fatal("liste vide acceptée")
	}
	if _, err := parseForbidden([]byte(`{`)); err == nil {
		t.Fatal("JSON cassé accepté")
	}
}

func send(t *testing.T, s *Store, channel, client, text string) *int {
	t.Helper()
	m, _, err := s.insertMessage(chat.Message{Channel: channel, ClientID: client, Nickname: client, Message: text})
	if err != nil {
		t.Fatal(err)
	}
	return m.SloubiNumber
}

func TestSloubiRule(t *testing.T) {
	s := testStore(t)
	if n := send(t, s, "c", "a", "sloubi 1"); n == nil || *n != 1 {
		t.Fatalf("sloubi 1 refusé")
	}
	if n := send(t, s, "c", "a", "sloubi 2"); n != nil {
		t.Fatalf("même auteur deux fois de suite accepté")
	}
	if n := send(t, s, "c", "b", "sloubi 3"); n != nil {
		t.Fatalf("mauvais numéro accepté")
	}
	if n := send(t, s, "c", "b", "SLOUBI 2 !"); n == nil || *n != 2 {
		t.Fatalf("sloubi 2 refusé")
	}
	if n := send(t, s, "c", "b", "sloubi"); n != nil {
		t.Fatalf("sloubi sans numéro accepté")
	}
}

func TestChannelsAreIsolated(t *testing.T) {
	s := testStore(t)
	send(t, s, "un", "a", "sloubi 1")
	if n := send(t, s, "deux", "a", "sloubi 1"); n == nil || *n != 1 {
		t.Fatalf("le compteur de deux dépend de un")
	}
	msgs, err := s.lastMessages("un", 100)
	if err != nil || len(msgs) != 1 || msgs[0].Channel != "un" {
		t.Fatalf("backlog de un = %+v, %v", msgs, err)
	}
	if ok, _ := s.channelExists("trois"); ok {
		t.Fatal("salon fantôme")
	}
}

func TestConcurrentSloubiCountsOnce(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	valid := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m, _, err := s.insertMessage(chat.Message{Channel: "c", ClientID: string(rune('a' + i)), Message: "sloubi 1"})
			if err != nil {
				t.Error(err)
				return
			}
			if m.SloubiNumber != nil {
				mu.Lock()
				valid++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if valid != 1 {
		t.Fatalf("%d sloubi 1 validés en parallèle, attendu 1", valid)
	}
}

func TestMigrationMovesLegacyMessagesToChante(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.sqlite")
	s, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	// recrée le schéma v1 (sans channel) avec un message
	if _, err := s.DB.Exec(`DROP TABLE messages; DROP TABLE channels;
		CREATE TABLE messages (id INTEGER PRIMARY KEY AUTOINCREMENT, client_id TEXT, nickname TEXT, message TEXT, ts TEXT, sloubi_number INTEGER);
		INSERT INTO messages (client_id, nickname, message, ts, sloubi_number) VALUES ('a','Perceval','sloubi 1','2025-10-04T00:00:00Z',1);`); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	s, err = openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	ls, err := s.lastSloubi(DefaultChannel)
	if err != nil || ls == nil || *ls.SloubiNumber != 1 {
		t.Fatalf("dernier sloubi de chante = %+v, %v", ls, err)
	}
	if ok, _ := s.channelExists(DefaultChannel); !ok {
		t.Fatal("chante absent de channels")
	}
}

func TestPostit(t *testing.T) {
	s := testStore(t)
	p, created, err := s.setPostit("c", "bonjour", "a")
	if err != nil || !created || p.Text != "bonjour" {
		t.Fatalf("setPostit = %+v, %v, %v", p, created, err)
	}
	if _, created, _ := s.setPostit("c", "re", "b"); created {
		t.Fatal("salon recréé")
	}
	if got, _ := s.postit("c"); got.Text != "re" {
		t.Fatalf("postit = %q", got.Text)
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(0.001, 2, 3, 1e12)
	if !l.Allow("ip") || !l.Allow("ip") {
		t.Fatal("burst refusé")
	}
	for i := 0; i < 3; i++ {
		l.Allow("ip")
	}
	l.mu.Lock()
	l.buckets["ip"].tokens = 2 // même rechargé, un banni reste banni
	l.mu.Unlock()
	if l.Allow("ip") {
		t.Fatal("ban ignoré")
	}
	if !l.Allow("autre") {
		t.Fatal("autre IP pénalisée")
	}
}

func testServer(t *testing.T) *gin.Engine {
	t.Helper()
	return newTestServer(t).router()
}

const testAdmin = "admin@exemple.fr"

func newTestServer(t *testing.T) *server {
	t.Helper()
	s := &server{
		store: testStore(t), hub: NewHub(), forbidden: testForbidden(t),
		postLimit: NewLimiter(100, 100, 0, 0), postitLimit: NewLimiter(100, 100, 0, 0),
		checkLimit: NewLimiter(100, 100, 0, 0), wsLimit: NewLimiter(100, 100, 0, 0),
		createLimit: NewLimiter(100, 100, 0, 0), lockLimit: NewLimiter(100, 100, 0, 0), reactLimit: NewLimiter(100, 100, 0, 0), uploadLimit: NewLimiter(100, 100, 0, 0),
		codeLimit: NewLimiter(100, 100, 0, 0), mail: testMailer{},
	}
	if err := s.store.ensureAdmin(testAdmin); err != nil {
		t.Fatal(err)
	}
	return s
}

// testMailer garde le dernier code envoyé à chaque adresse, au lieu de l'envoyer.
type testMailer struct{}

var sentCodes sync.Map

func (testMailer) SendCode(_ context.Context, to, code string) error {
	sentCodes.Store(to, code)
	return nil
}

// login : la vraie connexion par code, renvoie le cookie de session.
func login(t *testing.T, r http.Handler, email string) []*http.Cookie {
	t.Helper()
	if w := do(r, "POST", "/api/_compte/code", `{"email":"`+email+`"}`); w.Code != 200 {
		t.Fatalf("code pour %s = %d %s", email, w.Code, w.Body.String())
	}
	code, _ := sentCodes.Load(email)
	w := do(r, "POST", "/api/_compte/verify", `{"email":"`+email+`","code":"`+code.(string)+`"}`)
	if w.Code != 200 {
		t.Fatalf("verify %s = %d %s", email, w.Code, w.Body.String())
	}
	return w.Result().Cookies()
}

// adminCookies : seul l'admin (ou un membre validé) pose un mot de passe.
func adminCookies(t *testing.T, r http.Handler) []*http.Cookie {
	t.Helper()
	return login(t, r, testAdmin)
}

func do(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRoutes(t *testing.T) {
	r := testServer(t)
	cases := []struct {
		method, path, body string
		code               int
		contains           string
	}{
		{"GET", "/", "", 200, `id="chanInput"`},
		{"GET", "/chante", "", 200, `data-chan="chante"`},
		{"GET", "/chante", "", 200, `id="smoke"`},
		{"GET", "/kaamelott", "", 200, `id="smoke"`},
		{"GET", "/Chante", "", 301, ""},
		{"GET", "/admin", "", 404, `data-prefill="admin"`},
		{"GET", "/a/b", "", 404, `id="chanInput"`},
		{"GET", "/app.js", "", 200, "sendMessage"},
		{"GET", "/assets/room.css", "", 200, ""},
		{"GET", "/forbidden.json", "", 404, ""},
		{"GET", "/api/check/kaamelott", "", 200, `"ok":true`},
		{"GET", "/api/check/4dm1n", "", 200, `"reason":"forbidden"`},
		{"GET", "/api/check/a--b", "", 200, `"reason":"invalid"`},
		{"POST", "/api/kaamelott/messages", `{"clientId":"a","nickname":"Perceval","message":"sloubi 1"}`, 202, ""},
		{"GET", "/api/check/kaamelott", "", 200, `"exists":true`},
		{"GET", "/api/kaamelott/last-sloubi", "", 200, `"number":1`},
		{"GET", "/api/chante/last-sloubi", "", 200, `"number":0`},
		{"POST", "/api/kaamelott/messages", `{"nickname":"Testeur","message":"   "}`, 400, ""},
		{"POST", "/api/kaamelott/messages", `{"nickname":"Testeur","message":"` + strings.Repeat("x", 501) + `"}`, 400, ""},
		{"POST", "/api/admin/messages", `{"nickname":"Testeur","message":"hop"}`, 404, ""},
		{"POST", "/api/kaamelott/postit", `{"text":"Ici on compte."}`, 200, `"text":"Ici on compte."`},
		{"POST", "/api/kaamelott/postit", `{"text":"` + strings.Repeat("x", 281) + `"}`, 400, ""},
		{"POST", "/verif.json", `{"nickname":"Testeur","clientId":"a","message":"sloubi 1"}`, 202, ""},
		{"GET", "/last-sloubi.json", "", 200, `"number":1`},
		{"GET", "/api/nope", "", 404, `"error"`},
		{"POST", "/api/nick", `{"nickname":"  Perceval   le  Gallois "}`, 200, `"nickname":"Perceval le Gallois"`},
		{"POST", "/api/nick", `{"nickname":"4dm1n"}`, 200, `"reason":"forbidden"`},
		{"POST", "/api/kaamelott/messages", `{"clientId":"z","nickname":"Modo","message":"je suis le chef"}`, 400, "nickname required"},
		{"POST", "/api/kaamelott/messages", `{"clientId":"z","nickname":"Anonyme","message":"coucou"}`, 400, "nickname required"},
		{"POST", "/api/kaamelott/messages", `{"clientId":"z","message":"coucou"}`, 400, "nickname required"},
		{"POST", "/api/nick", `{"nickname":"anonyme"}`, 200, `"reason":"forbidden"`},
	}
	for _, tc := range cases {
		w := do(r, tc.method, tc.path, tc.body)
		if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.contains) {
			t.Errorf("%s %s = %d %.120q, want %d containing %q", tc.method, tc.path, w.Code, w.Body.String(), tc.code, tc.contains)
		}
	}

	// sans application/json : refusé (un formulaire d'un site tiers ne peut pas l'envoyer)
	req := httptest.NewRequest("POST", "/api/kaamelott/messages", strings.NewReader(`{"nickname":"Testeur","message":"x"}`))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain = %d", w.Code)
	}
}

func TestCleanText(t *testing.T) {
	cases := []struct {
		in       string
		newlines bool
		want     string
	}{
		{" a\x00b\u2028c\r\nd ", true, "abc\nd"},
		{"a\nb", false, "ab"},
		{"\u202eecnevreP", false, "ecnevreP"},                   // inversion de direction
		{"sl\u200boubi 1", false, "sloubi 1"},                   // espace sans chasse
		{"\u2066gauche\u2069", false, "gauche"},                 // isolats de direction
		{"Z\u0351\u0352\u0357\u0358a", false, "Z\u0351\u0352a"}, // zalgo plafonné à 2
		{"e\u0301", false, "é"},                                 // NFC
		{"👨\u200d👩\u200d👧", false, "👨\u200d👩\u200d👧"},           // emoji composé intact
		{"❤️", false, "❤️"},
	}
	for _, tc := range cases {
		if got := cleanText(tc.in, tc.newlines); got != tc.want {
			t.Errorf("cleanText(%q) = %q, attendu %q", tc.in, got, tc.want)
		}
	}
}

func TestClientKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ remote, want string }{
		{"203.0.113.7:1234", "203.0.113.7"},
		{"[2001:db8:1:2:aaaa::1]:443", "2001:db8:1:2::/64"},
		{"[2001:db8:1:2:bbbb::9]:443", "2001:db8:1:2::/64"}, // même /64 : même clé
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.RemoteAddr = tc.remote
		if got := clientKey(c); got != tc.want {
			t.Errorf("clientKey(%s) = %s, attendu %s", tc.remote, got, tc.want)
		}
	}
}

func TestWSCap(t *testing.T) {
	s := &server{}
	for i := 0; i < maxWSPerKey; i++ {
		if !s.wsAcquire("ip") {
			t.Fatalf("connexion %d refusée", i)
		}
	}
	if s.wsAcquire("ip") {
		t.Fatal("plafond par IP ignoré")
	}
	if !s.wsAcquire("autre") {
		t.Fatal("autre IP pénalisée")
	}
	s.wsRelease("ip")
	if !s.wsAcquire("ip") {
		t.Fatal("place libérée non réutilisable")
	}
}

func TestSecurityHeaders(t *testing.T) {
	w := do(testServer(t), "GET", "/kaamelott", "")
	for h, want := range map[string]string{
		"Content-Security-Policy":    "media-src 'self' blob:;",
		"Strict-Transport-Security":  "max-age=",
		"X-Frame-Options":            "DENY",
		"X-Content-Type-Options":     "nosniff",
		"Permissions-Policy":         "camera=(self), microphone=(self), display-capture=(self), geolocation=()",
		"Cross-Origin-Opener-Policy": "same-origin",
	} {
		if !strings.Contains(w.Header().Get(h), want) {
			t.Errorf("%s = %q, attendu %q", h, w.Header().Get(h), want)
		}
	}
	if strings.Contains(w.Body.String(), `style="`) {
		t.Error("style inline dans la page : bloqué par la CSP")
	}
}

func TestNickname(t *testing.T) {
	s := &server{forbidden: testForbidden(t)}
	cases := []struct{ in, want, reason string }{
		{"Perceval", "Perceval", ""},
		{"  Karadoc   de  Vannes ", "Karadoc de Vannes", ""},
		{"Léodagan", "Léodagan", ""},
		{"Le\u0301odagan", "Léodagan", ""}, // accent décomposé → recomposé (NFC)
		{"Dame_Séli-2.0", "Dame_Séli-2.0", ""},
		{"O'Brien", "O'Brien", ""},
		{"Реrceval", "", nickChars},       // P et e cyrilliques
		{"Perce\u200bval", "", nickChars}, // espace sans chasse
		{"\u202eecnevreP", "", nickChars}, // inversion de direction
		{"Z\u0351\u0352\u0357algo", "", nickChars},
		{"<b>Arthur</b>", "", nickChars},
		{"😈", "", nickChars},
		{"A", "", nickTooShort},
		{"", "", nickTooShort},
		{strings.Repeat("a", 25), "", nickTooLong},
		{"Admin", "", nickForbidden},
		{"m0d0", "", nickForbidden},
		{"Coca Cola", "", nickForbidden},
		{"L'Oréal", "", nickForbidden},
		{"Nike", "", nickForbidden},
		{"Karadoc de Vannes", "Karadoc de Vannes", ""},
		{"Anonyme", "", nickForbidden},
	}
	for _, tc := range cases {
		got, reason := s.cleanNickname(tc.in)
		if got != tc.want || reason != tc.reason {
			t.Errorf("cleanNickname(%q) = %q, %q ; attendu %q, %q", tc.in, got, reason, tc.want, tc.reason)
		}
	}
}

func TestDirectory(t *testing.T) {
	r := testServer(t)
	if w := do(r, "GET", "/_/", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Aucun salon") {
		t.Fatalf("annuaire vide = %d", w.Code)
	}

	r = testServer(t) // cache neuf
	do(r, "POST", "/api/table-ronde/messages", `{"clientId":"a","nickname":"Arthur","message":"sloubi 1"}`)
	do(r, "POST", "/api/table-ronde/messages", `{"clientId":"b","nickname":"Lancelot","message":"sloubi 2"}`)
	do(r, "POST", "/api/kaamelott/postit", `{"text":"Ici on compte."}`)
	w := do(r, "GET", "/_/", "")
	body := w.Body.String()
	for _, want := range []string{`href="/table-ronde"`, `href="/kaamelott"`, "<strong>2</strong> salons", "<strong>2</strong> sloubis", "à l’instant"} {
		if !strings.Contains(body, want) {
			t.Errorf("annuaire sans %q", want)
		}
	}
	if w.Header().Get("X-Robots-Tag") == "" {
		t.Error("annuaire indexable")
	}
	if w := do(r, "GET", "/_", ""); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/_/" {
		t.Errorf("/_ = %d → %q", w.Code, w.Header().Get("Location"))
	}
	// aucun salon ne peut commencer par un séparateur : pas de collision avec /_/
	f := testForbidden(t)
	for _, n := range []string{"_", "_x", ".x", "-x", "__"} {
		if f.checkChannel(n) == "" {
			t.Errorf("salon %q accepté", n)
		}
	}
}

func TestAgo(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{{10 * time.Second, "à l’instant"}, {5 * time.Minute, "il y a 5 min"}, {3 * time.Hour, "il y a 3 h"}, {72 * time.Hour, "il y a 3 j"}} {
		if got := ago(time.Now().Add(-tc.d).UTC().Format(tsLayout)); got != tc.want {
			t.Errorf("ago(-%v) = %q, attendu %q", tc.d, got, tc.want)
		}
	}
}

func TestExtractTags(t *testing.T) {
	got := extractTags("Les #Ronces et la #permaculture, encore #ronces ; pas c#, pas #x, ni a#b. #Forêt-Nourricière #b1 #b2 #b3 #b4")
	want := []string{"ronces", "permaculture", "forêt-nourricière"}
	if len(got) != maxTagsPerMessage || strings.Join(got[:3], ",") != strings.Join(want, ",") {
		t.Fatalf("extractTags = %q", got)
	}
}

func TestPassword(t *testing.T) {
	h, err := hashPassword("sésame")
	if err != nil {
		t.Fatal(err)
	}
	if !checkPassword(h, "sésame") || checkPassword(h, "sesame") || checkPassword("n'importe quoi", "sésame") {
		t.Fatal("checkPassword")
	}
	if h2, _ := hashPassword("sésame"); h2 == h {
		t.Fatal("sel non aléatoire")
	}
}

// req : une requête avec en-têtes et cookies, pour le créateur et les visiteurs.
func req(r http.Handler, method, path, body string, cookies []*http.Cookie, headers ...string) *httptest.ResponseRecorder {
	rq := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		rq.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		rq.Header.Set(headers[i], headers[i+1])
	}
	for _, c := range cookies {
		rq.AddCookie(c)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, rq)
	return w
}

func jsonField(t *testing.T, w *httptest.ResponseRecorder, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("JSON %q: %v", w.Body.String(), err)
	}
	s, _ := m[key].(string)
	return s
}

func TestRoomOwnerThemeAndPassword(t *testing.T) {
	r := testServer(t)

	// le premier message fonde le lieu et rend la clé du créateur, une seule fois
	w := do(r, "POST", "/api/clairiere/messages", `{"clientId":"a","nickname":"Merlin","message":"bonjour"}`)
	key := jsonField(t, w, "ownerKey")
	if w.Code != 202 || len(key) < 40 {
		t.Fatalf("fondation = %d %s", w.Code, w.Body.String())
	}
	if w := do(r, "POST", "/api/clairiere/messages", `{"nickname":"Testeur","clientId":"b","message":"re"}`); strings.Contains(w.Body.String(), "ownerKey") {
		t.Fatal("clé donnée à un second visiteur")
	}
	if w := req(r, "GET", "/api/clairiere/info", "", nil, "X-Owner-Key", key); !strings.Contains(w.Body.String(), `"owner":true`) || !strings.Contains(w.Body.String(), `"messages":2`) {
		t.Fatalf("info créateur = %s", w.Body.String())
	}
	if w := do(r, "GET", "/api/clairiere/info", ""); strings.Contains(w.Body.String(), `"owner":true`) {
		t.Fatal("tout le monde est créateur")
	}

	// seul le créateur règle le lieu
	if w := do(r, "POST", "/api/clairiere/settings", `{"theme":"foret"}`); w.Code != 403 {
		t.Fatalf("réglage sans clé = %d", w.Code)
	}
	if w := do(r, "POST", "/api/clairiere/settings", `{"ownerKey":"`+key+`","theme":"jungle"}`); w.Code != 400 {
		t.Fatalf("thème inconnu = %d", w.Code)
	}
	if w := do(r, "POST", "/api/clairiere/settings", `{"ownerKey":"`+key+`","theme":"foret"}`); w.Code != 200 {
		t.Fatalf("thème = %d %s", w.Code, w.Body.String())
	}
	if w := do(r, "GET", "/clairiere", ""); !strings.Contains(w.Body.String(), `data-theme="foret"`) || !strings.Contains(w.Body.String(), "forest-fx.js") {
		t.Fatal("page sans le décor de la forêt")
	}
	// pas de sloubi hors de la taverne ; les #mots deviennent des microbiotes
	do(r, "POST", "/api/clairiere/messages", `{"nickname":"Testeur","clientId":"a","message":"sloubi 1 #ronces"}`)
	if w := do(r, "GET", "/api/clairiere/last-sloubi", ""); !strings.Contains(w.Body.String(), `"number":0`) {
		t.Fatalf("sloubi compté dans la forêt : %s", w.Body.String())
	}
	if w := do(r, "GET", "/api/clairiere/microbiotes", ""); !strings.Contains(w.Body.String(), `{"tag":"ronces","count":1}`) {
		t.Fatalf("microbiotes = %s", w.Body.String())
	}
	if w := do(r, "GET", "/api/clairiere/microbiotes/Ronces", ""); !strings.Contains(w.Body.String(), "sloubi 1 #ronces") {
		t.Fatalf("encyclopédie = %s", w.Body.String())
	}

	// mot de passe : le créateur reçoit son cookie, les autres trouvent porte close
	w = req(r, "POST", "/api/clairiere/settings", `{"ownerKey":"`+key+`","password":"ab"}`, adminCookies(t, r))
	if w.Code != 400 {
		t.Fatalf("mot de passe trop court = %d", w.Code)
	}
	w = req(r, "POST", "/api/clairiere/settings", `{"ownerKey":"`+key+`","password":"sésame ouvre-toi"}`, adminCookies(t, r))
	owner := w.Result().Cookies()
	if w.Code != 200 || len(owner) != 1 || !owner[0].HttpOnly {
		t.Fatalf("verrou = %d, cookies %v", w.Code, owner)
	}
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/clairiere/messages", `{"nickname":"Testeur","message":"coucou"}`},
		{"POST", "/api/clairiere/postit", `{"text":"coucou"}`},
		{"GET", "/api/clairiere/last-sloubi", ""},
		{"GET", "/api/clairiere/microbiotes", ""},
		{"GET", "/ws/clairiere", ""},
	} {
		if w := do(r, tc.method, tc.path, tc.body); w.Code != 403 {
			t.Errorf("%s %s sans mot de passe = %d", tc.method, tc.path, w.Code)
		}
	}
	if w := do(r, "GET", "/clairiere", ""); !strings.Contains(w.Body.String(), "data-locked") {
		t.Error("page sans la porte")
	}
	if w := do(r, "GET", "/api/clairiere/info", ""); strings.Contains(w.Body.String(), "messages") || !strings.Contains(w.Body.String(), `"locked":true`) {
		t.Errorf("info fuit derrière la porte : %s", w.Body.String())
	}
	if w := do(r, "GET", "/api/check/clairiere", ""); !strings.Contains(w.Body.String(), `"locked":true`) || !strings.Contains(w.Body.String(), `"theme":"foret"`) {
		t.Errorf("check = %s", w.Body.String())
	}
	if w := do(r, "GET", "/api/check/clairiere", ""); !strings.Contains(w.Body.String(), `"access":false`) {
		t.Errorf("check sans cookie = %s", w.Body.String())
	}
	if w := req(r, "GET", "/api/check/clairiere", "", owner); !strings.Contains(w.Body.String(), `"access":true`) {
		t.Errorf("check avec cookie = %s", w.Body.String())
	}
	if w := req(r, "POST", "/api/clairiere/messages", `{"nickname":"Testeur","message":"c'est moi"}`, owner); w.Code != 202 {
		t.Errorf("créateur refusé = %d", w.Code)
	}
	if w := do(r, "POST", "/api/clairiere/unlock", `{"password":"sesame ouvre-toi"}`); w.Code != 403 {
		t.Errorf("mauvais mot de passe = %d", w.Code)
	}
	w = do(r, "POST", "/api/clairiere/unlock", `{"password":"sésame ouvre-toi"}`)
	visitor := w.Result().Cookies()
	if w.Code != 200 || len(visitor) != 1 {
		t.Fatalf("déverrouillage = %d", w.Code)
	}
	if w := req(r, "POST", "/api/clairiere/messages", `{"nickname":"Testeur","message":"me voilà"}`, visitor); w.Code != 202 {
		t.Errorf("visiteur déverrouillé refusé = %d", w.Code)
	}
	forged := []*http.Cookie{{Name: visitor[0].Name, Value: "AAAA"}}
	if w := req(r, "POST", "/api/clairiere/messages", `{"nickname":"Testeur","message":"faux"}`, forged); w.Code != 403 {
		t.Errorf("cookie forgé accepté = %d", w.Code)
	}

	// changer le mot de passe invalide les anciens cookies ; le retirer rouvre le lieu
	req(r, "POST", "/api/clairiere/settings", `{"ownerKey":"`+key+`","password":"autre chose"}`, adminCookies(t, r))
	if w := req(r, "POST", "/api/clairiere/messages", `{"nickname":"Testeur","message":"encore"}`, visitor); w.Code != 403 {
		t.Errorf("ancien cookie toujours valable = %d", w.Code)
	}
	req(r, "POST", "/api/clairiere/settings", `{"ownerKey":"`+key+`","removePassword":true}`, adminCookies(t, r))
	if w := do(r, "POST", "/api/clairiere/messages", `{"nickname":"Testeur","message":"ouvert"}`); w.Code != 202 {
		t.Errorf("lieu rouvert = %d", w.Code)
	}

	// un lieu neuf peut être fondé par ses réglages ; /chante n'a pas de créateur
	w = do(r, "POST", "/api/bord-de-l-eau/settings", `{"theme":"moulin"}`)
	if w.Code != 200 || jsonField(t, w, "ownerKey") == "" {
		t.Fatalf("fondation par réglages = %d %s", w.Code, w.Body.String())
	}
	if w := do(r, "GET", "/bord-de-l-eau", ""); !strings.Contains(w.Body.String(), "mill-fx.js") {
		t.Error("page sans le moulin")
	}
	if w := do(r, "POST", "/api/bord-de-l-eau/settings", `{"theme":"taverne"}`); w.Code != 403 {
		t.Errorf("refondation = %d", w.Code)
	}
	do(r, "POST", "/api/chante/messages", `{"nickname":"Testeur","message":"hop"}`)
	if w := do(r, "POST", "/api/chante/settings", `{"theme":"foret"}`); w.Code != 403 {
		t.Errorf("/chante réglable = %d", w.Code)
	}
}

func TestDirectoryHidesLockedCounts(t *testing.T) {
	r := testServer(t)
	key := jsonField(t, do(r, "POST", "/api/secret/messages", `{"nickname":"Testeur","clientId":"a","message":"sloubi 1"}`), "ownerKey")
	req(r, "POST", "/api/secret/settings", `{"ownerKey":"`+key+`","password":"motdepasse"}`, adminCookies(t, r))
	body := do(r, "GET", "/_/", "").Body.String()
	if !strings.Contains(body, `href="/secret"`) || !strings.Contains(body, "🔒") || strings.Contains(body, "<strong>1</strong> sloubis") {
		t.Fatalf("annuaire : %s", body)
	}
}

func TestTurnCredentials(t *testing.T) {
	s := newTestServer(t)
	s.turn = turnConfig{secret: []byte("sel"), host: "turn.example", port: "3478"}
	r := s.router()
	key := jsonField(t, do(r, "POST", "/api/ouvert/messages", `{"nickname":"Testeur","message":"hop"}`), "ownerKey")
	if w := do(r, "GET", "/api/ouvert/turn", ""); w.Code != 403 {
		t.Fatalf("TURN sans mot de passe = %d", w.Code)
	}
	owner := req(r, "POST", "/api/ouvert/settings", `{"ownerKey":"`+key+`","password":"motdepasse"}`, adminCookies(t, r)).Result().Cookies()
	if w := do(r, "GET", "/api/ouvert/turn", ""); w.Code != 403 {
		t.Fatalf("TURN sans cookie = %d", w.Code)
	}
	w := req(r, "GET", "/api/ouvert/turn", "", owner)
	var got struct {
		IceServers []struct {
			URLs       any    `json:"urls"`
			Username   string `json:"username"`
			Credential string `json:"credential"`
		} `json:"iceServers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got.IceServers) != 2 {
		t.Fatalf("TURN = %d %s", w.Code, w.Body.String())
	}
	tu := got.IceServers[1]
	m := hmac.New(sha1.New, []byte("sel"))
	m.Write([]byte(tu.Username))
	if tu.Credential != base64.StdEncoding.EncodeToString(m.Sum(nil)) || !strings.HasSuffix(tu.Username, ":sloubi") {
		t.Fatalf("identifiants coturn invalides : %+v", tu)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("identifiants mis en cache")
	}
	// la page n'offre l'appel qu'à qui a le mot de passe
	if !strings.Contains(req(r, "GET", "/ouvert", "", owner).Body.String(), "call.js") {
		t.Error("pas d'appel pour le créateur")
	}
	if strings.Contains(do(r, "GET", "/ouvert", "").Body.String(), "call.js") {
		t.Error("appel proposé derrière la porte")
	}
}

// wsClient : un membre du salon, connecté pour de vrai.
type wsClient struct {
	t    *testing.T
	conn *websocket.Conn
}

func dialRoom(t *testing.T, srv *httptest.Server, channel string, cookies []*http.Cookie) *wsClient {
	t.Helper()
	h := http.Header{}
	for _, c := range cookies {
		h.Add("Cookie", c.String())
	}
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/"+channel, h)
	if err != nil {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Fatalf("ws %s : %v (%d)", channel, err, code)
	}
	t.Cleanup(func() { conn.Close() })
	return &wsClient{t, conn}
}

func (w *wsClient) send(v string) {
	w.t.Helper()
	if err := w.conn.WriteMessage(websocket.TextMessage, []byte(v)); err != nil {
		w.t.Fatal(err)
	}
}

// expect lit jusqu'à un événement d'appel de l'opération voulue.
func (w *wsClient) expect(op string) map[string]any {
	w.t.Helper()
	w.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, b, err := w.conn.ReadMessage()
		if err != nil {
			w.t.Fatalf("attendu %q : %v", op, err)
		}
		var m map[string]any
		json.Unmarshal(b, &m)
		if m["type"] == "call" && m["op"] == op {
			return m
		}
	}
}

func TestCallSignaling(t *testing.T) {
	s := newTestServer(t)
	srv := httptest.NewServer(s.router())
	defer srv.Close()
	r := s.router()

	key := jsonField(t, do(r, "POST", "/api/veillee/messages", `{"nickname":"Testeur","message":"hop"}`), "ownerKey")
	pass := req(r, "POST", "/api/veillee/settings", `{"ownerKey":"`+key+`","password":"motdepasse"}`, adminCookies(t, r)).Result().Cookies()

	a := dialRoom(t, srv, "veillee", pass)
	b := dialRoom(t, srv, "veillee", pass)
	watcher := dialRoom(t, srv, "veillee", pass) // dans le salon, pas dans l'appel

	a.send(`{"type":"call","op":"join","nick":"Arthur"}`)
	ja := a.expect("joined")
	if peers := ja["peers"].([]any); len(peers) != 0 {
		t.Fatalf("premier arrivé voit %v", peers)
	}
	if roster := watcher.expect("roster")["peers"].([]any); len(roster) != 1 {
		t.Fatalf("roster = %v", roster)
	}

	b.send(`{"type":"call","op":"join","nick":"Guenièvre"}`)
	jb := b.expect("joined")
	peers := jb["peers"].([]any)
	if len(peers) != 1 || peers[0].(map[string]any)["id"] != ja["you"] || peers[0].(map[string]any)["nick"] != "Arthur" {
		t.Fatalf("le nouveau venu voit %v", peers)
	}

	// la signalisation va au destinataire, et à lui seul, avec l'expéditeur réel
	b.send(`{"type":"call","op":"signal","to":"` + ja["you"].(string) + `","data":{"sdp":{"type":"offer","sdp":"v=0"}}}`)
	sig := a.expect("signal")
	if sig["from"] != jb["you"] || sig["data"].(map[string]any)["sdp"] == nil {
		t.Fatalf("signal = %v", sig)
	}
	watcher.send(`{"type":"call","op":"signal","to":"` + ja["you"].(string) + `","data":{"x":1}}`) // pas dans l'appel : ignoré

	// couper son micro se voit ; partir aussi
	a.send(`{"type":"call","op":"state","muted":true}`)
	if p := watcher.expect("roster")["peers"].([]any); len(p) != 2 {
		t.Fatalf("roster après b = %v", p)
	}
	b.conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p := watcher.expect("roster")["peers"].([]any); len(p) == 1 && p[0].(map[string]any)["muted"] == true {
			return
		}
	}
	t.Fatal("le départ de b ou le micro coupé de a n'ont pas été annoncés")
}

func TestCallLimits(t *testing.T) {
	s := newTestServer(t)
	srv := httptest.NewServer(s.router())
	defer srv.Close()
	r := s.router()

	// salon ouvert : pas d'appel
	do(r, "POST", "/api/place/messages", `{"nickname":"Testeur","message":"hop"}`)
	open := dialRoom(t, srv, "place", nil)
	open.send(`{"type":"call","op":"join","nick":"Perceval"}`)
	open.expect("denied")

	// six au plus
	key := jsonField(t, do(r, "POST", "/api/table/messages", `{"nickname":"Testeur","message":"hop"}`), "ownerKey")
	pass := req(r, "POST", "/api/table/settings", `{"ownerKey":"`+key+`","password":"motdepasse"}`, adminCookies(t, r)).Result().Cookies()
	for i := 0; i < maxCallers; i++ {
		c := dialRoom(t, srv, "table", pass)
		c.send(`{"type":"call","op":"join","nick":"Chevalier"}`)
		c.expect("joined")
	}
	extra := dialRoom(t, srv, "table", pass)
	extra.send(`{"type":"call","op":"join","nick":"Septième"}`)
	extra.expect("full")

	// sans le mot de passe, pas de socket du tout
	h := http.Header{}
	if _, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/table", h); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Fatal("WS ouvert sans mot de passe")
	}
}

// livesOf : qui est en live d'après un roster, dans l'ordre (« a,b »).
func livesOf(m map[string]any) string {
	var ids []string
	l, _ := m["lives"].([]any)
	for _, x := range l {
		ids = append(ids, x.(string))
	}
	return strings.Join(ids, ",")
}

func TestCallStage(t *testing.T) {
	s := newTestServer(t)
	srv := httptest.NewServer(s.router())
	defer srv.Close()
	r := s.router()
	key := jsonField(t, do(r, "POST", "/api/scene/messages", `{"nickname":"Testeur","message":"hop"}`), "ownerKey")
	pass := req(r, "POST", "/api/scene/settings", `{"ownerKey":"`+key+`","password":"motdepasse"}`, adminCookies(t, r)).Result().Cookies()

	watcher := dialRoom(t, srv, "scene", pass)
	watcher.send(`{"type":"call","op":"live","on":true}`) // pas dans l'appel : ignoré
	join := func(nick string) (*wsClient, string) {
		c := dialRoom(t, srv, "scene", pass)
		c.send(`{"type":"call","op":"join","nick":"` + nick + `"}`)
		return c, c.expect("joined")["you"].(string)
	}
	a, aid := join("Arthur")
	b, bid := join("Bohort")
	d, did := join("Dagonet")
	waitLives := func(want string) {
		t.Helper()
		for i := 0; i < 12; i++ {
			m := watcher.expect("roster")
			if livesOf(m) == want {
				if first, _, _ := strings.Cut(want, ","); m["live"] != first { // l'ancien champ : la première personne
					t.Fatalf("live = %v, attendu %q", m["live"], first)
				}
				return
			}
		}
		t.Fatalf("en live attendu %q", want)
	}

	a.send(`{"type":"call","op":"live","on":true}`)
	waitLives(aid)
	b.send(`{"type":"call","op":"live","on":true}`) // une deuxième place : le duo
	waitLives(aid + "," + bid)
	d.send(`{"type":"call","op":"live","on":true}`) // pas de troisième
	d.expect("stage-busy")
	d.send(`{"type":"call","op":"live","on":false}`) // d ne fait descendre personne

	a.send(`{"type":"call","op":"leave"}`) // qui raccroche libère sa place
	waitLives(bid)
	d.send(`{"type":"call","op":"live","on":true}`)
	waitLives(bid + "," + did)
	b.send(`{"type":"call","op":"live","on":false}`)
	waitLives(did)
}

// decryptPush fait ce que fait le navigateur (RFC 8291), pour vérifier encryptPush.
func decryptPush(t *testing.T, body []byte, ua *ecdh.PrivateKey, auth []byte) []byte {
	t.Helper()
	salt, idlen := body[:16], int(body[20])
	asPub, ct := body[21:21+idlen], body[21+idlen:]
	as, err := ecdh.P256().NewPublicKey(asPub)
	if err != nil {
		t.Fatal(err)
	}
	shared, _ := ua.ECDH(as)
	prkKey, _ := hkdf.Extract(sha256.New, shared, auth)
	ikm, _ := hkdf.Expand(sha256.New, prkKey, "WebPush: info\x00"+string(ua.PublicKey().Bytes())+string(asPub), 32)
	prk, _ := hkdf.Extract(sha256.New, ikm, salt)
	cek, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		t.Fatalf("déchiffrement : %v", err)
	}
	if pt[len(pt)-1] != 0x02 {
		t.Fatal("délimiteur de dernier enregistrement absent")
	}
	return pt[:len(pt)-1]
}

func TestPushCrypto(t *testing.T) {
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	rand.Read(auth)
	body, err := encryptPush([]byte(`{"title":"Appel"}`), ua.PublicKey().Bytes(), auth)
	if err != nil {
		t.Fatal(err)
	}
	if got := decryptPush(t, body, ua, auth); string(got) != `{"title":"Appel"}` {
		t.Fatalf("contenu = %q", got)
	}

	// VAPID : un JWT ES256 que la clé publique annoncée vérifie, pour la bonne origine
	s := testStore(t)
	key, err := s.loadVAPID()
	if err != nil {
		t.Fatal(err)
	}
	again, _ := s.loadVAPID()
	if !key.Equal(again) {
		t.Fatal("clés VAPID non conservées")
	}
	authz, err := vapidAuth(key, "https://fcm.googleapis.com/fcm/send/abc", "https://sloubi.chat")
	if err != nil {
		t.Fatal(err)
	}
	var jwt, k string
	fmt.Sscanf(strings.ReplaceAll(authz, ",", ""), "vapid t=%s k=%s", &jwt, &k)
	parts := strings.Split(jwt, ".")
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	h := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(&key.PublicKey, h[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("signature VAPID invalide")
	}
	claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if !strings.Contains(string(claims), `"aud":"https://fcm.googleapis.com"`) {
		t.Fatalf("aud = %s", claims)
	}
	if pub, _ := key.PublicKey.Bytes(); k != base64.RawURLEncoding.EncodeToString(pub) {
		t.Fatal("k ne correspond pas à la clé")
	}
}

func TestPushHosts(t *testing.T) {
	for u, want := range map[string]bool{
		"https://fcm.googleapis.com/fcm/send/x":                true,
		"https://updates.push.services.mozilla.com/wpush/v2/x": true,
		"https://web.push.apple.com/x":                         true,
		"https://wns2-par02p.notify.windows.com/w/?token=x":    true,
		"http://fcm.googleapis.com/x":                          false,
		"https://fcm.googleapis.com:8443/x":                    false,
		"https://evil.example/fcm.googleapis.com":              false,
		"https://fcm.googleapis.com.evil.example/x":            false,
		"https://user@fcm.googleapis.com/x":                    false,
		"https://169.254.169.254/latest/meta-data":             false,
		"https://notpush.services.mozilla.com.attacker.net/x":  false,
	} {
		if got := pushHostAllowed(u); got != want {
			t.Errorf("pushHostAllowed(%q) = %v", u, got)
		}
	}
}

func TestCallStartPushes(t *testing.T) {
	// un faux service de push, qui garde ce qu'on lui envoie
	var mu sync.Mutex
	got := map[string][]byte{}
	push := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got[r.URL.Path] = b
		mu.Unlock()
		if r.Header.Get("Content-Encoding") != "aes128gcm" || !strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") || r.Header.Get("TTL") == "" {
			w.WriteHeader(400)
			return
		}
		if r.URL.Path == "/mort" {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(201)
	}))
	defer push.Close()

	s := newTestServer(t)
	s.push = newPusher(s.store)
	s.push.client = push.Client()
	s.push.allowed = func(u string) bool { return strings.HasPrefix(u, push.URL) }
	srv := httptest.NewServer(s.router())
	defer srv.Close()
	r := s.router()

	key := jsonField(t, do(r, "POST", "/api/veillee/messages", `{"nickname":"Testeur","message":"hop"}`), "ownerKey")
	pass := req(r, "POST", "/api/veillee/settings", `{"ownerKey":"`+key+`","password":"motdepasse"}`, adminCookies(t, r)).Result().Cookies()

	type browser struct {
		ua   *ecdh.PrivateKey
		auth []byte
	}
	subscribe := func(path, tag string) browser {
		ua, _ := ecdh.P256().GenerateKey(rand.Reader)
		auth := make([]byte, 16)
		rand.Read(auth)
		body := fmt.Sprintf(`{"endpoint":%q,"keys":{"p256dh":%q,"auth":%q},"tag":%q}`, push.URL+path,
			base64.RawURLEncoding.EncodeToString(ua.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(auth), tag)
		if w := req(r, "POST", "/api/veillee/push", body, pass); w.Code != 200 {
			t.Fatalf("abonnement %s = %d %s", path, w.Code, w.Body.String())
		}
		return browser{ua, auth}
	}
	absent := subscribe("/absent", "tag-absent-1")
	subscribe("/present", "tag-present-1")
	subscribe("/mort", "tag-mort-001")

	// sans le mot de passe, pas d'abonnement ; vers un hôte quelconque non plus
	if w := do(r, "POST", "/api/veillee/push", `{"endpoint":"https://fcm.googleapis.com/x","keys":{"p256dh":"x","auth":"y"},"tag":"abcdefgh"}`); w.Code != 403 {
		t.Errorf("abonnement sans cookie = %d", w.Code)
	}
	if w := req(r, "POST", "/api/veillee/push", `{"endpoint":"https://evil.example/x","keys":{"p256dh":"x","auth":"y"},"tag":"abcdefgh"}`, pass); w.Code != 400 {
		t.Errorf("abonnement vers un hôte quelconque = %d", w.Code)
	}

	present := dialRoom(t, srv, "veillee", pass)
	present.send(`{"type":"hello","push":"tag-present-1"}`)
	caller := dialRoom(t, srv, "veillee", pass)
	caller.send(`{"type":"call","op":"join","nick":"Lancelot"}`)
	caller.expect("joined")

	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if _, ok := got["/present"]; ok {
		t.Error("push envoyé à quelqu'un déjà dans le salon")
	}
	b, ok := got["/absent"]
	if !ok {
		t.Fatal("pas de push pour l'absent")
	}
	if msg := decryptPush(t, b, absent.ua, absent.auth); !strings.Contains(string(msg), "Lancelot") || !strings.Contains(string(msg), `"url":"/veillee"`) {
		t.Errorf("contenu = %s", msg)
	}
	// l'abonnement mort (410) est oublié
	if subs, _ := s.store.pushSubs("veillee"); len(subs) != 2 {
		t.Errorf("abonnements restants = %d", len(subs))
	}
	// pas de rafale : un deuxième appel tout de suite ne repousse rien
	if !s.push.last["veillee"].After(time.Now().Add(-pushEvery)) {
		t.Error("limite de fréquence non posée")
	}
}

func TestSpectatorAndNickRequired(t *testing.T) {
	s := newTestServer(t)
	srv := httptest.NewServer(s.router())
	defer srv.Close()
	r := s.router()
	key := jsonField(t, do(r, "POST", "/api/regard/messages", `{"nickname":"Testeur","message":"hop"}`), "ownerKey")
	pass := req(r, "POST", "/api/regard/settings", `{"ownerKey":"`+key+`","password":"motdepasse"}`, adminCookies(t, r)).Result().Cookies()

	a := dialRoom(t, srv, "regard", pass)
	a.send(`{"type":"call","op":"join","nick":"Anonyme"}`)
	a.expect("nick-required")
	a.send(`{"type":"call","op":"join","nick":"Yvain","listen":true}`)
	a.expect("joined")
	for i := 0; i < 5; i++ {
		ro := a.expect("roster")
		if p := ro["peers"].([]any); len(p) == 1 && p[0].(map[string]any)["listen"] == true && p[0].(map[string]any)["muted"] == true {
			return
		}
	}
	t.Fatal("spectateur non annoncé")
}

func TestCallHandoff(t *testing.T) {
	s := newTestServer(t)
	srv := httptest.NewServer(s.router())
	defer srv.Close()
	r := s.router()
	key := jsonField(t, do(r, "POST", "/api/relais/messages", `{"nickname":"Testeur","message":"hop"}`), "ownerKey")
	pass := req(r, "POST", "/api/relais/settings", `{"ownerKey":"`+key+`","password":"motdepasse"}`, adminCookies(t, r)).Result().Cookies()

	join := func(nick string, listen bool) (*wsClient, string) {
		c := dialRoom(t, srv, "relais", pass)
		c.send(fmt.Sprintf(`{"type":"call","op":"join","nick":%q,"listen":%v}`, nick, listen))
		return c, c.expect("joined")["you"].(string)
	}
	a, aid := join("Arthur", false)
	b, bid := join("Bohort", false)
	c, cid := join("Calogrenant", true) // spectateur
	d, did := join("Dagonet", false)
	waitLives := func(want string) {
		t.Helper()
		for i := 0; i < 14; i++ {
			if livesOf(c.expect("roster")) == want {
				return
			}
		}
		t.Fatalf("en live attendu %q", want)
	}

	b.send(`{"type":"call","op":"handoff","to":"` + aid + `"}`) // pas en live : ignoré
	c.send(`{"type":"call","op":"live","on":true}`)             // un spectateur ne monte pas sur scène
	a.send(`{"type":"call","op":"live","on":true}`)
	waitLives(aid)
	d.send(`{"type":"call","op":"live","on":true}`)
	waitLives(aid + "," + did) // la scène est pleine

	a.send(`{"type":"call","op":"handoff","to":"` + cid + `"}`) // un spectateur n'a pas de micro : ignoré
	a.send(`{"type":"call","op":"handoff","to":"` + did + `"}`) // déjà en live : ignoré
	a.send(`{"type":"call","op":"handoff","to":"` + bid + `"}`)
	if h := b.expect("handoff"); h["from"] != aid || h["nick"] != "Arthur" {
		t.Fatalf("handoff = %v", h)
	}
	b.send(`{"type":"call","op":"handoff-decline"}`)
	if m := a.expect("handoff-declined"); m["nick"] != "Bohort" {
		t.Fatalf("refus = %v", m)
	}
	b.send(`{"type":"call","op":"live","on":true}`) // refusé, et la scène est pleine
	b.expect("stage-busy")

	a.send(`{"type":"call","op":"handoff","to":"` + bid + `"}`)
	b.expect("handoff")
	b.send(`{"type":"call","op":"live","on":true}`) // accepté : Bohort prend la place d'Arthur, Dagonet reste
	waitLives(did + "," + bid)
}

func TestThemesComplete(t *testing.T) {
	if len(themeOrder) != len(themes) {
		t.Fatalf("themeOrder (%d) et themes (%d) divergent", len(themeOrder), len(themes))
	}
	r := testServer(t)
	for _, id := range themeOrder {
		th := themes[id]
		if th.ID != id || th.Name == "" || th.CounterLabel == "" || th.Color == "" {
			t.Errorf("thème %q incomplet : %+v", id, th)
		}
		if _, err := os.Stat("assets/" + th.FX); err != nil {
			t.Errorf("thème %q : %v", id, err)
		}
		room := "essai-" + id
		key := jsonField(t, do(r, "POST", "/api/"+room+"/settings", `{"theme":"`+id+`"}`), "ownerKey")
		if key == "" {
			t.Fatalf("thème %q refusé", id)
		}
		if body := do(r, "GET", "/"+room, "").Body.String(); !strings.Contains(body, `data-theme="`+id+`"`) || !strings.Contains(body, th.FX) {
			t.Errorf("page du thème %q incomplète", id)
		}
	}
}

func TestExtractLinks(t *testing.T) {
	got := extractLinks("Va voir https://exemple.fr/page?a=1, et (www.sloubi.chat/chante) ou http://x.io/a_(b). Pas ftp://non ni javascript:alert(1).")
	want := []string{"https://exemple.fr/page?a=1", "www.sloubi.chat/chante", "http://x.io/a_(b)"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("extractLinks = %q", got)
	}
}

func TestLikesAndClicks(t *testing.T) {
	r := testServer(t)
	do(r, "POST", "/api/liens/messages", `{"nickname":"Arthur","message":"Lisez https://a.fr et https://b.fr"}`)
	do(r, "POST", "/api/autre/messages", `{"nickname":"Arthur","message":"ailleurs"}`)

	// un like, un autre, puis on en retire un ; jamais sous zéro
	for _, tc := range []struct {
		body string
		code int
		want string
	}{
		{`{"id":1,"delta":1}`, 200, `"likes":1`},
		{`{"id":1,"delta":1}`, 200, `"likes":2`},
		{`{"id":1,"delta":-1}`, 200, `"likes":1`},
		{`{"id":1,"delta":5}`, 400, ""},
		{`{"id":2,"delta":1}`, 404, ""}, // le message d'un autre salon
	} {
		if w := do(r, "POST", "/api/liens/like", tc.body); w.Code != tc.code || !strings.Contains(w.Body.String(), tc.want) {
			t.Errorf("like %s = %d %s", tc.body, w.Code, w.Body.String())
		}
	}
	do(r, "POST", "/api/autre/like", `{"id":2,"delta":-1}`)
	do(r, "POST", "/api/autre/like", `{"id":2,"delta":-1}`)
	if w := do(r, "POST", "/api/autre/like", `{"id":2,"delta":1}`); !strings.Contains(w.Body.String(), `"likes":1`) {
		t.Errorf("like sous zéro : %s", w.Body.String())
	}

	// les clics : seulement sur des liens qui existent
	do(r, "POST", "/api/liens/click", `{"id":1,"i":1}`)
	if w := do(r, "POST", "/api/liens/click", `{"id":1,"i":1}`); !strings.Contains(w.Body.String(), `"clicks":2`) {
		t.Errorf("clic = %s", w.Body.String())
	}
	if w := do(r, "POST", "/api/liens/click", `{"id":1,"i":2}`); w.Code != 400 {
		t.Errorf("clic sur un lien inexistant = %d", w.Code)
	}

	// le backlog porte les totaux : on les relit comme le fait le WebSocket
	st := testStore(t)
	m, _, _ := st.insertMessage(chat.Message{Channel: "c", Nickname: "A", Message: "https://a.fr"})
	st.like(m.ID, 1)
	st.click(m.ID, 0)
	st.click(m.ID, 0)
	msgs, err := st.lastMessages("c", 10)
	if err != nil || len(msgs) != 1 || msgs[0].Likes != 1 || msgs[0].Clicks["0"] != 2 {
		t.Fatalf("backlog = %+v, %v", msgs, err)
	}
}

func TestClassify(t *testing.T) {
	jpg := []byte("\xFF\xD8\xFF\xE0\x00\x10JFIF\x00")
	png := []byte("\x89PNG\r\n\x1a\n")
	for _, tc := range []struct {
		name string
		head []byte
		kind string
	}{
		{"photo.JPG", jpg, "image"},
		{"capture.png", png, "image"},
		// un vrai PNG dont les métadonnées (manifeste C2PA) contiennent le texte « <svg »
		{"genere.png", append(append([]byte{}, png...), []byte("\x00\x00\x00\x0dIHDRcaBXjumbc2pa<svg xmlns=...>")...), "image"},
		{"film.mov", []byte("\x00\x00\x00\x14ftypqt  "), "video"},
		{"son.mp3", []byte("ID3\x03\x00"), "audio"},
		{"Message vocal.weba", []byte("\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\xf7\x81\x01\x42\xf2\x81\x04\x42\xf3\x81\x08\x42\x82\x84webm"), "audio"},
		{"memo.m4a", []byte("\x00\x00\x00\x1cftypM4A \x00\x00\x00\x00isommp42"), "audio"},
		{"clip.webm", []byte("\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\xf7\x81\x01\x42\xf2\x81\x04\x42\xf3\x81\x08\x42\x82\x84webm"), "video"},
		{"devis.pdf", []byte("%PDF-1.7"), "document"},
		{"piege.png", []byte("<!DOCTYPE html><script>alert(1)</script>"), "document"},
		{"dessin.svg", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg">`), "document"},
		{"faux.jpg", []byte("%PDF-1.7"), "document"},
		{"inconnu.bin", []byte{0, 1, 2, 3}, "document"},
	} {
		if kind, _ := classify(tc.name, tc.head); kind != tc.kind {
			t.Errorf("classify(%q) = %s, attendu %s", tc.name, kind, tc.kind)
		}
	}
	if n := cleanFileName("../../etc/passwd"); n != "passwd" {
		t.Errorf("cleanFileName = %q", n)
	}
}

func TestJPEGOrientation(t *testing.T) {
	// un en-tête JPEG minimal avec un bloc EXIF (TIFF big-endian, une entrée : orientation = 6)
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, 6, 0, 0, 0, 0, 0, 0, 0, 0}
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	head := []byte{0xFF, 0xD8, 0xFF, 0xE1, byte((len(app1) + 2) >> 8), byte(len(app1) + 2)}
	head = append(head, app1...)
	if o := jpegOrientation(head); o != 6 {
		t.Fatalf("orientation = %d", o)
	}
	if o := jpegOrientation([]byte("pas un jpeg")); o != 1 {
		t.Fatalf("orientation par défaut = %d", o)
	}
}

func TestUploads(t *testing.T) {
	uploadRoot = t.TempDir()
	s := newTestServer(t)
	srv := httptest.NewServer(s.router())
	defer srv.Close()
	r := s.router()

	do(r, "POST", "/api/ouvert/messages", `{"nickname":"Arthur","message":"hop"}`)
	key := jsonField(t, do(r, "POST", "/api/coffre/messages", `{"nickname":"Arthur","message":"hop"}`), "ownerKey")
	pass := req(r, "POST", "/api/coffre/settings", `{"ownerKey":"`+key+`","password":"motdepasse"}`, adminCookies(t, r)).Result().Cookies()

	send := func(channel, name string, body []byte, cookies []*http.Cookie) *http.Response {
		t.Helper()
		rq, _ := http.NewRequest("POST", srv.URL+"/api/"+channel+"/files", bytes.NewReader(body))
		rq.Header.Set("X-File-Name", url.QueryEscape(name))
		rq.Header.Set("X-Nickname", "Arthur")
		for _, c := range cookies {
			rq.AddCookie(c)
		}
		resp, err := http.DefaultClient.Do(rq)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	get := func(path string, cookies []*http.Cookie) (*http.Response, []byte) {
		t.Helper()
		rq, _ := http.NewRequest("GET", srv.URL+path, nil)
		for _, c := range cookies {
			rq.AddCookie(c)
		}
		resp, err := http.DefaultClient.Do(rq)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, b
	}

	// une vraie photo, 1200×800
	img := image.NewRGBA(image.Rect(0, 0, 1200, 800))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	var photo bytes.Buffer
	jpeg.Encode(&photo, img, nil)

	if resp := send("ouvert", "photo.jpg", photo.Bytes(), nil); resp.StatusCode != 403 {
		t.Fatalf("envoi dans un lieu ouvert = %d", resp.StatusCode)
	}
	if resp := send("coffre", "photo.jpg", photo.Bytes(), nil); resp.StatusCode != 403 {
		t.Fatalf("envoi sans mot de passe = %d", resp.StatusCode)
	}
	resp := send("coffre", "Vacances été.jpg", photo.Bytes(), pass)
	var up struct {
		ID   int64        `json:"id"`
		File chat.FileRef `json:"file"`
	}
	json.NewDecoder(resp.Body).Decode(&up)
	resp.Body.Close()
	if resp.StatusCode != 201 || up.File.Kind != "image" || !up.File.Thumb || up.File.W != 1200 || up.File.Name != "Vacances été.jpg" {
		t.Fatalf("envoi = %d %+v", resp.StatusCode, up)
	}

	// servie en ligne, avec sa miniature ; en téléchargement sur demande ; jamais sans le cookie
	rs, b := get("/api/coffre/files/"+up.File.ID, pass)
	if rs.StatusCode != 200 || rs.Header.Get("Content-Type") != "image/jpeg" || len(b) != photo.Len() || !strings.HasPrefix(rs.Header.Get("Content-Disposition"), "inline") {
		t.Fatalf("fichier = %d %v %d", rs.StatusCode, rs.Header, len(b))
	}
	rs, b = get("/api/coffre/files/"+up.File.ID+"?thumb=1", pass)
	if th, err := jpeg.DecodeConfig(bytes.NewReader(b)); err != nil || th.Width != thumbMax {
		t.Fatalf("miniature = %v %+v", err, th)
	}
	rs, _ = get("/api/coffre/files/"+up.File.ID+"?dl=1", pass)
	if !strings.HasPrefix(rs.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("téléchargement = %q", rs.Header.Get("Content-Disposition"))
	}
	if rs, _ := get("/api/coffre/files/"+up.File.ID, nil); rs.StatusCode != 403 {
		t.Errorf("fichier sans cookie = %d", rs.StatusCode)
	}

	// du HTML déguisé en image : rangé en document, servi en pièce jointe, jamais exécuté
	resp = send("coffre", "chat.png", []byte("<!DOCTYPE html><script>alert(1)</script>"), pass)
	json.NewDecoder(resp.Body).Decode(&up)
	resp.Body.Close()
	if up.File.Kind != "document" {
		t.Fatalf("HTML déguisé rangé en %s", up.File.Kind)
	}
	rs, _ = get("/api/coffre/files/"+up.File.ID, pass)
	if rs.Header.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(rs.Header.Get("Content-Disposition"), "attachment") || !strings.Contains(rs.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("HTML servi avec %v", rs.Header)
	}

	// la médiathèque range tout seule
	_, b = get("/api/coffre/files?kind=image", pass)
	if !bytes.Contains(b, []byte(`"counts":{"document":1,"image":1,"video":0}`)) || !bytes.Contains(b, []byte(`Vacances été.jpg`)) {
		t.Errorf("médiathèque = %s", b)
	}

	// le backlog porte le fichier
	msgs, _ := s.store.lastMessages("coffre", 10)
	if last := msgs[len(msgs)-1]; last.File == nil || last.File.Kind != "document" {
		t.Errorf("backlog sans fichier : %+v", last)
	}

	// favoris : le plus aimé d'abord, ou ceux qu'on a aimés soi-même
	req(r, "POST", "/api/coffre/like", `{"id":`+strconv.FormatInt(msgs[1].ID, 10)+`,"delta":1}`, pass)
	if _, b := get("/api/coffre/favorites", pass); !bytes.Contains(b, []byte(`"likes":1`)) || !bytes.Contains(b, []byte(`"kind":"image"`)) {
		t.Errorf("favoris = %s", b)
	}

	// le quota : 10 Go par salon
	s.store.DB.Exec(`INSERT INTO files (id, channel, name, mime, kind, size, created_at) VALUES ('x', 'coffre', 'gros', 'x', 'document', ?, 'x')`, roomQuota)
	if resp := send("coffre", "encore.txt", []byte("un peu plus"), pass); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("au-delà du quota = %d", resp.StatusCode)
	}
}

func TestReclassify(t *testing.T) {
	uploadRoot = t.TempDir()
	st := testStore(t)
	// un PNG (avec de la transparence) enregistré comme document par une version précédente
	img := image.NewNRGBA(image.Rect(0, 0, 900, 600))
	var buf bytes.Buffer
	png.Encode(&buf, img)
	os.MkdirAll(filepath.Join(uploadRoot, "salon"), 0o750)
	os.WriteFile(filePath("salon", "abc"), buf.Bytes(), 0o640)
	st.DB.Exec(`INSERT INTO files (id, channel, name, mime, kind, size, created_at) VALUES ('abc', 'salon', 'x.png', 'application/octet-stream', 'document', ?, 'x')`, buf.Len())
	st.reclassify()
	f, err := st.fileByID("salon", "abc")
	if err != nil || f.Kind != "image" || f.Mime != "image/png" || !f.Thumb || f.W != 900 {
		t.Fatalf("après reclassement : %+v, %v", f, err)
	}
	// la transparence devient du blanc dans la miniature, pas du noir
	th, _ := os.Open(filePath("salon", "abc") + ".thumb.jpg")
	defer th.Close()
	timg, err := jpeg.Decode(th)
	if err != nil {
		t.Fatal(err)
	}
	if r, _, _, _ := timg.At(10, 10).RGBA(); r < 0xf000 {
		t.Errorf("transparence rendue en %x, attendu du blanc", r)
	}
}

func TestVideoLinks(t *testing.T) {
	for link, want := range map[string]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":           "youtube dQw4w9WgXcQ",
		"https://youtube.com/watch?feature=share&v=dQw4w9WgXcQ": "youtube dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ?t=42":                     "youtube dQw4w9WgXcQ",
		"https://m.youtube.com/shorts/dQw4w9WgXcQ":              "youtube dQw4w9WgXcQ",
		"https://vimeo.com/76979871":                            "vimeo 76979871",
		"https://www.dailymotion.com/video/x8abc12":             "dailymotion x8abc12",
		"https://dai.ly/x8abc12":                                "dailymotion x8abc12",
		"https://www.youtube.com/channel/UCabc":                 "",
		"https://vimeo.com/about":                               "",
		"https://exemple.fr/video.mp4":                          "",
	} {
		p, id, ok := videoOf(link)
		if got := strings.TrimSpace(p + " " + id); (ok && got != want) || (!ok && want != "") {
			t.Errorf("videoOf(%q) = %q, attendu %q", link, got, want)
		}
	}

	uploadRoot = t.TempDir()
	vthumbRoot = t.TempDir()
	s := newTestServer(t)
	r := s.router()
	key := jsonField(t, do(r, "POST", "/api/cine/messages", `{"nickname":"Arthur","message":"hop"}`), "ownerKey")
	pass := req(r, "POST", "/api/cine/settings", `{"ownerKey":"`+key+`","password":"motdepasse"}`, adminCookies(t, r)).Result().Cookies()
	for _, m := range []string{
		"Regardez https://youtu.be/dQw4w9WgXcQ et lisez https://exemple.fr/article",
		"Encore https://www.youtube.com/watch?v=dQw4w9WgXcQ (la même) et www.sloubi.chat/chante",
		"Et https://vimeo.com/76979871",
	} {
		req(r, "POST", "/api/cine/messages", `{"nickname":"Arthur","message":"`+m+`"}`, pass)
	}
	videos, webs, err := s.store.sharedLinks("cine", 50)
	if err != nil || len(videos) != 2 || len(webs) != 2 {
		t.Fatalf("liens = %+v / %+v, %v", videos, webs, err)
	}
	if videos[0].Provider != "vimeo" || videos[1].URL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" || videos[1].Idx != 0 {
		t.Errorf("vidéos = %+v", videos)
	}
	if webs[0].Host != "sloubi.chat" || webs[0].URL != "https://www.sloubi.chat/chante" || webs[1].Host != "exemple.fr" || webs[1].Idx != 1 {
		t.Errorf("liens = %+v", webs)
	}

	// les comptes du menu incluent les liens
	w := req(r, "GET", "/api/cine/files?kind=document", "", pass)
	if !strings.Contains(w.Body.String(), `"document":2`) || !strings.Contains(w.Body.String(), `"video":2`) || !strings.Contains(w.Body.String(), `"host":"exemple.fr"`) {
		t.Errorf("médiathèque = %s", w.Body.String())
	}

	// pas de miniature pour une vidéo jamais citée ici : le serveur n'est pas un relais ouvert
	if w := req(r, "GET", "/api/cine/vthumb/youtube/aaaaaaaaaaa", "", pass); w.Code != 404 {
		t.Errorf("miniature d'une vidéo non citée = %d", w.Code)
	}
	if w := req(r, "GET", "/api/cine/vthumb/ailleurs/dQw4w9WgXcQ", "", pass); w.Code != 404 {
		t.Errorf("plateforme inconnue = %d", w.Code)
	}
	if w := do(r, "GET", "/api/cine/vthumb/youtube/dQw4w9WgXcQ", ""); w.Code != 403 {
		t.Errorf("miniature sans mot de passe = %d", w.Code)
	}
	if videoHostAllowed(&url.URL{Scheme: "https", Host: "169.254.169.254"}) || videoHostAllowed(&url.URL{Scheme: "http", Host: "i.ytimg.com"}) {
		t.Error("hôte non autorisé accepté")
	}
}

// l'exemple publié doit suffire à démarrer, et bloquer les routes du site
func TestForbiddenExample(t *testing.T) {
	f, err := loadForbidden("forbidden.example.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app.js", "sw.js", "api", "super.admin"} {
		if f.checkChannel(name) == "" {
			t.Errorf("%q accepté par l'exemple", name)
		}
	}
	if f.checkChannel("kaamelott") != "" || f.checkChannel("badminton-club") != "" {
		t.Error("l'exemple refuse un nom ordinaire")
	}
}

func TestAdminClaimsExistingRooms(t *testing.T) {
	st := testStore(t)
	for _, name := range []string{"ancien", "fonde"} {
		if _, err := st.createRoom(name, "", DefaultTheme); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.ensureAdmin("moi@exemple.fr"); err != nil {
		t.Fatal(err)
	}
	admin, err := st.userByEmail("moi@exemple.fr")
	if err != nil || admin.Role != roleAdmin || admin.Status != statusApproved {
		t.Fatalf("admin = %+v, %v", admin, err)
	}
	for _, name := range []string{"ancien", "fonde"} {
		if r, _ := st.room(name); r.OwnerUser != admin.ID {
			t.Errorf("%s appartient à %d", name, r.OwnerUser)
		}
	}
	// une seule fois : les lieux fondés ensuite restent à leur créateur
	st.createRoom("nouveau", "h", DefaultTheme)
	st.ensureAdmin("moi@exemple.fr")
	if r, _ := st.room("nouveau"); r.OwnerUser != 0 {
		t.Errorf("nouveau lieu pris par l'admin")
	}
	// un seul admin : changer d'adresse rétrograde l'ancienne
	st.ensureAdmin("autre@exemple.fr")
	if u, _ := st.userByEmail("moi@exemple.fr"); u.Role != roleMember {
		t.Errorf("deux admins")
	}
}

func TestLoginCodes(t *testing.T) {
	r := testServer(t)
	if w := do(r, "POST", "/api/_compte/code", `{"email":"pas-une-adresse"}`); w.Code != 400 {
		t.Errorf("adresse invalide = %d", w.Code)
	}
	if w := do(r, "GET", "/api/_compte/me", ""); !strings.Contains(w.Body.String(), `"signedIn":false`) {
		t.Errorf("me sans session = %s", w.Body.String())
	}
	if w := do(r, "POST", "/api/_compte/code", `{"email":"Lea@Exemple.fr "}`); w.Code != 200 {
		t.Fatalf("code = %d", w.Code)
	}
	if w := do(r, "POST", "/api/_compte/code", `{"email":"lea@exemple.fr"}`); w.Code != 429 {
		t.Errorf("renvoi immédiat = %d", w.Code)
	}
	code, _ := sentCodes.Load("lea@exemple.fr")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 0; i < otpMaxAttempts; i++ {
		if w := do(r, "POST", "/api/_compte/verify", `{"email":"lea@exemple.fr","code":"`+wrong+`"}`); w.Code != 401 {
			t.Fatalf("mauvais code = %d", w.Code)
		}
	}
	// cinq essais ratés : même le bon code ne passe plus
	if w := do(r, "POST", "/api/_compte/verify", `{"email":"lea@exemple.fr","code":"`+code.(string)+`"}`); w.Code != 401 {
		t.Errorf("bon code après 5 échecs = %d", w.Code)
	}
	session := login(t, r, "lea@exemple.fr")
	w := req(r, "GET", "/api/_compte/me", "", session)
	if !strings.Contains(w.Body.String(), `"email":"lea@exemple.fr"`) || !strings.Contains(w.Body.String(), `"status":"aucun"`) {
		t.Errorf("me = %s", w.Body.String())
	}
	forged := []*http.Cookie{{Name: sessionCookie, Value: session[0].Value + "x"}}
	if w := req(r, "GET", "/api/_compte/me", "", forged); !strings.Contains(w.Body.String(), `"signedIn":false`) {
		t.Errorf("session forgée acceptée")
	}
	if w := req(r, "GET", "/api/_admin/users", "", session); w.Code != 403 {
		t.Errorf("liste des comptes pour un membre = %d", w.Code)
	}
	if w := do(r, "GET", "/_/compte", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "account.js") {
		t.Errorf("page compte = %d", w.Code)
	}
}

func TestPasswordNeedsApproval(t *testing.T) {
	r := testServer(t)
	key := jsonField(t, do(r, "POST", "/api/jardin/messages", `{"nickname":"Testeur","message":"hop"}`), "ownerKey")
	// sans compte : il faut se connecter
	if w := do(r, "POST", "/api/jardin/settings", `{"ownerKey":"`+key+`","password":"motdepasse"}`); w.Code != 401 {
		t.Fatalf("mot de passe sans compte = %d", w.Code)
	}
	// connecté mais pas validé : c'est une demande, le lieu reste ouvert
	lea := login(t, r, "lea@exemple.fr")
	w := req(r, "POST", "/api/jardin/settings", `{"ownerKey":"`+key+`","password":"motdepasse"}`, lea)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"pending":true`) || !strings.Contains(w.Body.String(), `"locked":false`) {
		t.Fatalf("demande = %d %s", w.Code, w.Body.String())
	}
	if w := do(r, "POST", "/api/jardin/messages", `{"nickname":"Testeur","message":"encore ouvert"}`); w.Code != 202 {
		t.Errorf("lieu fermé avant la validation = %d", w.Code)
	}
	if w := req(r, "GET", "/api/jardin/info", "", lea); !strings.Contains(w.Body.String(), `"pending":true`) || !strings.Contains(w.Body.String(), `"owner":true`) {
		t.Errorf("info = %s", w.Body.String())
	}
	// le lieu est désormais à son compte : la clé seule ne suffit plus
	if w := do(r, "POST", "/api/jardin/settings", `{"ownerKey":"`+key+`","theme":"moulin"}`); w.Code != 403 {
		t.Errorf("clé de créateur après le compte = %d", w.Code)
	}

	admin := adminCookies(t, r)
	w = req(r, "GET", "/api/_admin/users", "", admin)
	if !strings.Contains(w.Body.String(), `"email":"lea@exemple.fr"`) || !strings.Contains(w.Body.String(), `"status":"en-attente"`) ||
		!strings.Contains(w.Body.String(), `"name":"jardin"`) || strings.Contains(w.Body.String(), "motdepasse") {
		t.Fatalf("demandes = %s", w.Body.String())
	}
	leaID := userID(t, r, admin, "lea@exemple.fr")
	if w := req(r, "POST", "/api/_admin/users/"+leaID, `{"action":"n'importe"}`, admin); w.Code != 400 {
		t.Errorf("action inconnue = %d", w.Code)
	}
	if w := req(r, "POST", "/api/_admin/users/"+leaID, `{"action":"valider"}`, lea); w.Code != 403 {
		t.Errorf("auto-validation = %d", w.Code)
	}
	if w := req(r, "POST", "/api/_admin/users/"+leaID, `{"action":"valider"}`, admin); w.Code != 200 || !strings.Contains(w.Body.String(), "jardin") {
		t.Fatalf("validation = %d %s", w.Code, w.Body.String())
	}
	// validé : le mot de passe demandé devient la serrure
	if w := do(r, "POST", "/api/jardin/messages", `{"nickname":"Testeur","message":"fermé ?"}`); w.Code != 403 {
		t.Errorf("lieu ouvert après validation = %d", w.Code)
	}
	if w := do(r, "POST", "/api/jardin/unlock", `{"password":"motdepasse"}`); w.Code != 200 {
		t.Errorf("le mot de passe demandé n'ouvre pas = %d", w.Code)
	}
	if w := req(r, "POST", "/api/jardin/settings", `{"password":"nouveau secret"}`, lea); !strings.Contains(w.Body.String(), `"pending":false`) || !strings.Contains(w.Body.String(), `"locked":true`) {
		t.Errorf("membre validé = %s", w.Body.String())
	}

	// retirer l'accès : le lieu reste gardé (sinon ses conversations deviendraient publiques)
	req(r, "POST", "/api/_admin/users/"+leaID, `{"action":"retirer"}`, admin)
	if w := do(r, "POST", "/api/jardin/messages", `{"nickname":"Testeur","message":"toujours fermé"}`); w.Code != 403 {
		t.Errorf("lieu rouvert par le retrait = %d", w.Code)
	}
	if w := req(r, "POST", "/api/jardin/settings", `{"password":"encore un autre"}`, lea); !strings.Contains(w.Body.String(), `"pending":true`) {
		t.Errorf("après retrait, nouveau mot de passe = %s", w.Body.String())
	}

	// refuser : la demande tombe
	key2 := jsonField(t, do(r, "POST", "/api/verger/messages", `{"nickname":"Testeur","message":"hop"}`), "ownerKey")
	tom := login(t, r, "tom@exemple.fr")
	req(r, "POST", "/api/verger/settings", `{"ownerKey":"`+key2+`","password":"motdepasse"}`, tom)
	req(r, "POST", "/api/_admin/users/"+userID(t, r, admin, "tom@exemple.fr"), `{"action":"refuser"}`, admin)
	if w := req(r, "GET", "/api/verger/info", "", tom); !strings.Contains(w.Body.String(), `"pending":false`) || !strings.Contains(w.Body.String(), `"status":"refuse"`) {
		t.Errorf("après refus = %s", w.Body.String())
	}

	// l'admin règle tous les lieux
	if w := req(r, "POST", "/api/verger/settings", `{"theme":"bd"}`, admin); w.Code != 200 {
		t.Errorf("admin sur un lieu d'autrui = %d", w.Code)
	}
}

// userID : l'id d'un compte, d'après la liste de l'admin.
func userID(t *testing.T, r http.Handler, admin []*http.Cookie, email string) string {
	t.Helper()
	var list struct{ Users []adminUser }
	json.Unmarshal(req(r, "GET", "/api/_admin/users", "", admin).Body.Bytes(), &list)
	for _, u := range list.Users {
		if u.Email == email {
			return strconv.FormatInt(u.ID, 10)
		}
	}
	t.Fatalf("%s absent de la liste", email)
	return ""
}

func hostReq(r http.Handler, host, method, path, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	rq := httptest.NewRequest(method, path, strings.NewReader(body))
	rq.Host = host
	if body != "" {
		rq.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		rq.AddCookie(c)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, rq)
	return w
}

func TestCustomDomains(t *testing.T) {
	s := newTestServer(t)
	s.siteURL, s.siteHosts = "https://sloubi.chat", map[string]bool{"sloubi.chat": true, "www.sloubi.chat": true, "example.com": true}
	r := s.router()
	key := jsonField(t, do(r, "POST", "/api/maison/messages", `{"nickname":"Testeur","message":"hop"}`), "ownerKey")

	// réservé aux membres
	if w := do(r, "POST", "/api/maison/settings", `{"ownerKey":"`+key+`","domain":"chez-nous.fr"}`); w.Code != 401 {
		t.Errorf("domaine sans compte = %d", w.Code)
	}
	admin := adminCookies(t, r)
	for _, bad := range []string{"pas un domaine", "localhost", "192.168.1.1", "sloubi.chat", "truc.sloubi.chat", "-x.fr", "x"} {
		if w := req(r, "POST", "/api/maison/settings", `{"domain":"`+bad+`"}`, admin); w.Code != 400 {
			t.Errorf("domaine %q accepté = %d", bad, w.Code)
		}
	}
	if w := req(r, "POST", "/api/maison/settings", `{"domain":"HTTPS://Chez-Nous.fr/"}`, admin); w.Code != 200 || jsonField(t, w, "domain") != "chez-nous.fr" {
		t.Fatalf("domaine = %d %s", w.Code, w.Body.String())
	}
	do(r, "POST", "/api/voisin/messages", `{"nickname":"Testeur","message":"hop"}`)
	if w := req(r, "POST", "/api/voisin/settings", `{"domain":"chez-nous.fr"}`, admin); w.Code != 409 {
		t.Errorf("domaine en double = %d", w.Code)
	}

	// sur le domaine : / est le lieu, l'API passe, le reste repart vers le site
	w := hostReq(r, "chez-nous.fr", "GET", "/", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `data-chan="maison"`) || !strings.Contains(w.Body.String(), `data-base="/"`) ||
		!strings.Contains(w.Body.String(), `href="https://sloubi.chat/"`) {
		t.Fatalf("lieu sur son domaine = %d", w.Code)
	}
	if w := hostReq(r, "Chez-Nous.fr:443", "POST", "/api/maison/messages", `{"nickname":"Testeur","message":"de chez nous"}`, nil); w.Code != 202 {
		t.Errorf("API sur le domaine = %d", w.Code)
	}
	if w := hostReq(r, "chez-nous.fr", "GET", "/assets/app.js", "", nil); w.Code != 200 {
		t.Errorf("assets sur le domaine = %d", w.Code)
	}
	for path, loc := range map[string]string{
		"/maison?heure=14": "/?heure=14",
		"/voisin":          "https://sloubi.chat/voisin",
		"/_/":              "https://sloubi.chat/_/",
	} {
		if w := hostReq(r, "chez-nous.fr", "GET", path, "", nil); w.Code != 302 || w.Header().Get("Location") != loc {
			t.Errorf("%s → %d %s, attendu %s", path, w.Code, w.Header().Get("Location"), loc)
		}
	}
	// domaine inconnu : vers le site
	if w := hostReq(r, "inconnu.fr", "GET", "/maison", "", nil); w.Code != 302 || w.Header().Get("Location") != "https://sloubi.chat/" || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("domaine inconnu = %d %s", w.Code, w.Header().Get("Location"))
	}
	// le site lui-même ne change pas
	if w := hostReq(r, "sloubi.chat", "GET", "/maison", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `data-base="/maison"`) {
		t.Errorf("site = %d", w.Code)
	}

	// derrière le relais de nadaprod : Host 127.0.0.1:8888, le domaine dans X-Forwarded-Host
	relay := func(remote string) *httptest.ResponseRecorder {
		rq := httptest.NewRequest("GET", "/", nil)
		rq.Host, rq.RemoteAddr = "127.0.0.1:8888", remote
		rq.Header.Set("X-Forwarded-Host", "chez-nous.fr")
		rq.Header.Set("X-Forwarded-For", "127.0.0.1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, rq)
		return w
	}
	if w := relay("127.0.0.1:4242"); w.Code != 200 || !strings.Contains(w.Body.String(), `data-chan="maison"`) {
		t.Errorf("relais local = %d", w.Code)
	}
	if w := relay("203.0.113.9:4242"); strings.Contains(w.Body.String(), `data-chan="maison"`) {
		t.Error("X-Forwarded-Host cru d'un inconnu")
	}
	ws := httptest.NewRequest("GET", "/ws/maison", nil)
	ws.Host, ws.RemoteAddr = "127.0.0.1:8888", "127.0.0.1:4242"
	ws.Header.Set("X-Forwarded-Host", "chez-nous.fr")
	ws.Header.Set("Origin", "https://chez-nous.fr")
	if !sameOrigin(ws) {
		t.Error("WebSocket refusé derrière le relais")
	}
	ws.Header.Set("Origin", "https://ailleurs.fr")
	if sameOrigin(ws) {
		t.Error("WebSocket d'un autre site accepté")
	}

	// TLS à la demande : le proxy local demande, l'extérieur n'obtient rien
	ask := func(d, remote string) int {
		rq := httptest.NewRequest("GET", "/_/domaine?domain="+d, nil)
		rq.RemoteAddr = remote
		w := httptest.NewRecorder()
		r.ServeHTTP(w, rq)
		return w.Code
	}
	if ask("chez-nous.fr", "127.0.0.1:5555") != 200 || ask("inconnu.fr", "127.0.0.1:5555") != 404 || ask("chez-nous.fr", "203.0.113.9:5555") != 404 {
		t.Error("ask du proxy")
	}

	// sur son domaine, le lieu est sa propre application : « Maison », initiale M
	page := hostReq(r, "chez-nous.fr", "GET", "/", "", nil).Body.String()
	if !strings.Contains(page, "<title>Maison</title>") || !strings.Contains(page, `apple-mobile-web-app-title" content="Maison"`) {
		t.Error("titre de l'application")
	}
	w = hostReq(r, "chez-nous.fr", "GET", "/manifest.json", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"Maison"`) || !strings.Contains(w.Body.String(), `"start_url":"/"`) {
		t.Errorf("manifeste = %d %s", w.Code, w.Body.String())
	}
	for path, size := range map[string]int{"/icon-512.png": 512, "/apple-touch-icon.png": 180, "/favicon.png": 64} {
		w := hostReq(r, "chez-nous.fr", "GET", path, "", nil)
		img, err := png.Decode(w.Body)
		if w.Code != 200 || err != nil || img.Bounds().Dx() != size {
			t.Errorf("%s = %d %v", path, w.Code, err)
		}
	}
	if w := hostReq(r, "sloubi.chat", "GET", "/manifest.json", "", nil); !strings.Contains(w.Body.String(), `"name": "Sloubi"`) {
		t.Error("le manifeste du site a changé")
	}

	// détacher
	req(r, "POST", "/api/maison/settings", `{"domain":""}`, admin)
	if w := hostReq(r, "chez-nous.fr", "GET", "/", "", nil); w.Code != 302 {
		t.Errorf("domaine détaché encore servi = %d", w.Code)
	}
}

// presenceEvent : le prochain événement « presence » reçu par w.
func (w *wsClient) presenceEvent() map[string]any {
	w.t.Helper()
	w.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, b, err := w.conn.ReadMessage()
		if err != nil {
			w.t.Fatalf("attendu une présence : %v", err)
		}
		var m map[string]any
		json.Unmarshal(b, &m)
		if m["type"] == "presence" {
			return m
		}
	}
}

func TestPresence(t *testing.T) {
	s := newTestServer(t)
	grace := 150 * time.Millisecond
	s.hub.presGrace = grace
	srv := httptest.NewServer(s.router())
	defer srv.Close()
	do(s.router(), "POST", "/api/feu/messages", `{"nickname":"Testeur","message":"hop"}`)
	hello := func(nick string) *wsClient {
		c := dialRoom(t, srv, "feu", nil)
		c.send(`{"type":"hello","nick":"` + nick + `"}`)
		return c
	}
	say := func(m map[string]any) string {
		if m["op"] == "here" {
			b, _ := json.Marshal(m["nicks"])
			return "here " + string(b)
		}
		return fmt.Sprint(m["op"], " ", m["nick"])
	}

	a := hello("Arthur")
	if got := say(a.presenceEvent()); got != "here []" {
		t.Fatalf("a : %s", got)
	}
	b := hello("Bohort")
	if got := say(b.presenceEvent()); got != `here ["Arthur"]` {
		t.Fatalf("b : %s", got)
	}
	if got := say(a.presenceEvent()); got != "in Bohort" {
		t.Fatalf("a voit : %s", got)
	}
	a2 := hello("Arthur") // un deuxième onglet : personne n'entre
	a2.presenceEvent()
	dialRoom(t, srv, "feu", nil).send(`{"type":"hello","nick":"x"}`) // pseudo invalide : ignoré

	// Bohort recharge sa page : ni sortie ni entrée
	b.conn.Close()
	b2 := hello("Bohort")
	b2.presenceEvent()
	time.Sleep(3 * grace)

	// Arthur ferme un onglet, puis l'autre : il sort, une seule fois, après le délai
	a.conn.Close()
	a2.conn.Close()
	if got := say(b2.presenceEvent()); got != "out Arthur" {
		t.Fatalf("b2 voit : %s (rien ne devait arriver avant)", got)
	}
	c := hello("Calogrenant")
	c.presenceEvent()
	if got := say(b2.presenceEvent()); got != "in Calogrenant" {
		t.Fatalf("b2 voit : %s", got)
	}
}
