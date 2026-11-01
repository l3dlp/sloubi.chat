package web

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const maxChannelLen = 32

// Séparateurs . _ - jamais doublés, ni au début ni à la fin.
var channelRe = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)

// Raisons renvoyées par /api/check et utilisées par l'accueil.
const (
	reasonInvalid   = "invalid"
	reasonTooLong   = "toolong"
	reasonForbidden = "forbidden"
)

var leet = strings.NewReplacer(
	"0", "o", "1", "i", "3", "e", "4", "a", "5", "s",
	"7", "t", "8", "b", "9", "g", "@", "a", "$", "s",
)

// normalizeForbidden ramène un nom (salon ou pseudo) à sa forme de comparaison :
// minuscules, accents retirés, leet converti, puis seulement a-z 0-9.
// "4dm1n", "super.admin", "Coca Cola" et "L'Oréal" deviennent
// "admin", "superadmin", "cocacola" et "loreal".
func normalizeForbidden(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue // diacritique détaché par NFD
		}
		b.WriteRune(r)
	}
	out := []rune(leet.Replace(b.String()))
	n := 0
	for _, r := range out {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out[n] = r
			n++
		}
	}
	return string(out[:n])
}

type forbiddenList struct {
	exact    map[string]struct{}
	contains []string
	allow    []string
}

func parseForbidden(data []byte) (*forbiddenList, error) {
	var raw struct {
		Exact    []string `json:"exact"`
		Contains []string `json:"contains"`
		Allow    []string `json:"allow"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if len(raw.Exact) == 0 && len(raw.Contains) == 0 {
		return nil, fmt.Errorf("liste vide")
	}
	f := &forbiddenList{exact: make(map[string]struct{}, len(raw.Exact))}
	for _, e := range raw.Exact {
		if n := normalizeForbidden(e); n != "" {
			f.exact[n] = struct{}{}
		}
	}
	for _, c := range raw.Contains {
		if n := normalizeForbidden(c); n != "" {
			f.contains = append(f.contains, n)
		}
	}
	for _, a := range raw.Allow {
		if n := normalizeForbidden(a); n != "" {
			f.allow = append(f.allow, n)
		}
	}
	return f, nil
}

func (f *forbiddenList) match(name string) bool {
	n := normalizeForbidden(name)
	if _, ok := f.exact[n]; ok {
		return true
	}
	for _, a := range f.allow {
		n = strings.ReplaceAll(n, a, "")
	}
	for _, c := range f.contains {
		if strings.Contains(n, c) {
			return true
		}
	}
	return false
}

// Forbidden garde la liste en mémoire et la recharge quand le fichier change.
// Un fichier cassé au rechargement est ignoré : on garde l'ancienne liste.
type Forbidden struct {
	path  string
	mu    sync.RWMutex
	list  *forbiddenList
	mtime time.Time
}

func loadForbidden(path string) (*Forbidden, error) {
	f := &Forbidden{path: path}
	if err := f.reload(); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *Forbidden) reload() error {
	st, err := os.Stat(f.path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(f.path)
	if err != nil {
		return err
	}
	list, err := parseForbidden(data)
	if err != nil {
		return fmt.Errorf("%s: %w", f.path, err)
	}
	f.mu.Lock()
	f.list, f.mtime = list, st.ModTime()
	f.mu.Unlock()
	return nil
}

func (f *Forbidden) watch(every time.Duration) {
	for range time.Tick(every) {
		st, err := os.Stat(f.path)
		if err != nil {
			continue
		}
		f.mu.RLock()
		changed := !st.ModTime().Equal(f.mtime)
		f.mu.RUnlock()
		if !changed {
			continue
		}
		if err := f.reload(); err != nil {
			log.Printf("forbidden: rechargement ignoré: %v", err)
		} else {
			log.Printf("forbidden: %s rechargé", f.path)
		}
	}
}

func (f *Forbidden) Match(name string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.list.match(name)
}

// checkChannel valide un nom déjà mis en minuscules. Renvoie "" si le nom est utilisable.
func (f *Forbidden) checkChannel(name string) string {
	switch {
	case len(name) > maxChannelLen:
		return reasonTooLong
	case !channelRe.MatchString(name):
		return reasonInvalid
	case f.Match(name):
		return reasonForbidden
	}
	return ""
}
