package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sloubi/src/chat"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
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
	s := &server{
		store: testStore(t), hub: NewHub(), forbidden: testForbidden(t),
		postLimit: NewLimiter(100, 100, 0, 0), postitLimit: NewLimiter(100, 100, 0, 0),
		checkLimit: NewLimiter(100, 100, 0, 0), wsLimit: NewLimiter(100, 100, 0, 0),
		createLimit: NewLimiter(100, 100, 0, 0),
	}
	return s.router()
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
		{"POST", "/api/kaamelott/messages", `{"clientId":"a","nickname":"P","message":"sloubi 1"}`, 202, ""},
		{"GET", "/api/check/kaamelott", "", 200, `"exists":true`},
		{"GET", "/api/kaamelott/last-sloubi", "", 200, `"number":1`},
		{"GET", "/api/chante/last-sloubi", "", 200, `"number":0`},
		{"POST", "/api/kaamelott/messages", `{"message":"   "}`, 400, ""},
		{"POST", "/api/kaamelott/messages", `{"message":"` + strings.Repeat("x", 501) + `"}`, 400, ""},
		{"POST", "/api/admin/messages", `{"message":"hop"}`, 404, ""},
		{"POST", "/api/kaamelott/postit", `{"text":"Ici on compte."}`, 200, `"text":"Ici on compte."`},
		{"POST", "/api/kaamelott/postit", `{"text":"` + strings.Repeat("x", 281) + `"}`, 400, ""},
		{"POST", "/verif.json", `{"clientId":"a","message":"sloubi 1"}`, 202, ""},
		{"GET", "/last-sloubi.json", "", 200, `"number":1`},
		{"GET", "/api/nope", "", 404, `"error"`},
		{"POST", "/api/nick", `{"nickname":"  Perceval   le  Gallois "}`, 200, `"nickname":"Perceval le Gallois"`},
		{"POST", "/api/nick", `{"nickname":"4dm1n"}`, 200, `"reason":"forbidden"`},
		{"POST", "/api/kaamelott/messages", `{"clientId":"z","nickname":"Modo","message":"je suis le chef"}`, 202, ""},
	}
	for _, tc := range cases {
		w := do(r, tc.method, tc.path, tc.body)
		if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.contains) {
			t.Errorf("%s %s = %d %.120q, want %d containing %q", tc.method, tc.path, w.Code, w.Body.String(), tc.code, tc.contains)
		}
	}

	// sans application/json : refusé (un formulaire d'un site tiers ne peut pas l'envoyer)
	req := httptest.NewRequest("POST", "/api/kaamelott/messages", strings.NewReader(`{"message":"x"}`))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain = %d", w.Code)
	}
}

func TestCleanText(t *testing.T) {
	if got := cleanText(" a\x00b c\r\nd ", true); got != "abc\nd" {
		t.Errorf("cleanText = %q", got)
	}
	if got := cleanText("a\nb", false); got != "ab" {
		t.Errorf("cleanText sans saut = %q", got)
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
		{"Anonyme", "Anonyme", ""},
	}
	for _, tc := range cases {
		got, reason := s.cleanNickname(tc.in)
		if got != tc.want || reason != tc.reason {
			t.Errorf("cleanNickname(%q) = %q, %q ; attendu %q, %q", tc.in, got, reason, tc.want, tc.reason)
		}
	}
}
