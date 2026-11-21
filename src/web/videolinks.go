package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

/* Les vidéos partagées en lien (YouTube, Vimeo, Dailymotion) rejoignent la
   vidéothèque, avec leur miniature et leur titre. C'est le serveur qui va les
   chercher, une fois, et qui les garde : les navigateurs ne parlent jamais à
   ces plateformes (pas d'adresse IP qui fuit, et la CSP reste img-src 'self').
   On ne va chercher que des adresses fixes chez ces trois-là, pour une vidéo
   réellement citée dans le salon : le serveur n'est pas un relais ouvert. */

const (
	videoMetaRetry = 24 * time.Hour // une plateforme muette : on réessaie le lendemain
	maxThumbBytes  = 3 << 20
	maxOEmbedBytes = 64 << 10
)

var vthumbRoot = filepath.Join("db", "vthumbs")

var (
	ytRe    = regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.|m\.|music\.)?(?:youtube\.com/(?:watch\?(?:[^#]*&)?v=|shorts/|live/|embed/)|youtu\.be/)([A-Za-z0-9_-]{11})`)
	vimeoRe = regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.|player\.)?vimeo\.com/(?:video/)?(\d{6,12})(?:[/?#]|$)`)
	dmRe    = regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.)?(?:dailymotion\.com/video/|dai\.ly/)([a-z0-9]{5,10})`)
	vidIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{5,12}$`)
)

// videoOf : la plateforme et l'identifiant d'un lien vidéo, s'il en est un.
func videoOf(link string) (provider, id string, ok bool) {
	if m := ytRe.FindStringSubmatch(link); m != nil {
		return "youtube", m[1], true
	}
	if m := vimeoRe.FindStringSubmatch(link); m != nil {
		return "vimeo", m[1], true
	}
	if m := dmRe.FindStringSubmatch(link); m != nil {
		return "dailymotion", strings.ToLower(m[1]), true
	}
	return "", "", false
}

func canonicalVideo(provider, id string) string {
	switch provider {
	case "youtube":
		return "https://www.youtube.com/watch?v=" + id
	case "vimeo":
		return "https://vimeo.com/" + id
	default:
		return "https://www.dailymotion.com/video/" + id
	}
}

// VideoLink : une vidéo partagée en lien, telle que l'affiche la vidéothèque.
type VideoLink struct {
	Provider  string `json:"provider"`
	ID        string `json:"id"`
	URL       string `json:"url"`
	Title     string `json:"title,omitempty"`
	Author    string `json:"author,omitempty"`
	MessageID int64  `json:"messageId"`
	Idx       int    `json:"i"` // la place du lien dans son message : pour compter le clic
	Nickname  string `json:"nickname"`
	Timestamp string `json:"timestamp"`
}

// WebLink : un lien partagé qui n'est pas une vidéo (rangé avec les documents).
type WebLink struct {
	URL       string `json:"url"`
	Host      string `json:"host"`
	MessageID int64  `json:"messageId"`
	Idx       int    `json:"i"`
	Clicks    int    `json:"clicks"`
	Nickname  string `json:"nickname"`
	Timestamp string `json:"timestamp"`
}

// sharedLinks : les liens partagés dans le salon, le plus récent d'abord, chacun une seule fois ;
// les vidéos d'un côté, le reste de l'autre.
func (s *Store) sharedLinks(channel string, limit int) ([]VideoLink, []WebLink, error) {
	rows, err := s.DB.Query(`SELECT m.id, m.nickname, m.message, m.ts FROM messages m
		WHERE m.channel = ? AND m.file_id IS NULL AND (m.message LIKE '%http%' OR m.message LIKE '%www.%')
		ORDER BY m.id DESC LIMIT 1000`, channel)
	if err != nil {
		return nil, nil, err
	}
	type found struct {
		id       int64
		nick, ts string
		links    []string
	}
	var msgs []found
	for rows.Next() {
		var f found
		var text string
		if err := rows.Scan(&f.id, &f.nick, &text, &f.ts); err != nil {
			rows.Close()
			return nil, nil, err
		}
		f.links = extractLinks(text)
		msgs = append(msgs, f)
	}
	rows.Close()
	videos, webs := []VideoLink{}, []WebLink{}
	seen := map[string]bool{}
	for _, f := range msgs {
		for i, link := range f.links {
			if p, vid, ok := videoOf(link); ok {
				if !seen[p+vid] && len(videos) < limit {
					seen[p+vid] = true
					videos = append(videos, VideoLink{Provider: p, ID: vid, URL: canonicalVideo(p, vid), MessageID: f.id, Idx: i, Nickname: f.nick, Timestamp: f.ts})
				}
				continue
			}
			href := link
			if strings.HasPrefix(strings.ToLower(href), "www.") {
				href = "https://" + href
			}
			u, err := url.Parse(href)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || seen[href] || len(webs) >= limit {
				continue
			}
			seen[href] = true
			webs = append(webs, WebLink{URL: href, Host: strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), MessageID: f.id, Idx: i, Nickname: f.nick, Timestamp: f.ts})
		}
	}
	for i := range videos {
		var title, author sql.NullString
		if s.DB.QueryRow(`SELECT title, author FROM video_meta WHERE provider = ? AND vid = ?`, videos[i].Provider, videos[i].ID).Scan(&title, &author) == nil {
			videos[i].Title, videos[i].Author = title.String, author.String
		}
	}
	for i := range webs {
		s.DB.QueryRow(`SELECT n FROM link_clicks WHERE message_id = ? AND idx = ?`, webs[i].MessageID, webs[i].Idx).Scan(&webs[i].Clicks)
	}
	return videos, webs, nil
}

// citesVideo : la vidéo est-elle bien citée dans ce salon ? (sinon, pas de miniature : pas de relais ouvert)
func (s *Store) citesVideo(channel, id string) bool {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE channel = ? AND message LIKE ?`, channel, "%"+id+"%").Scan(&n)
	return n > 0
}

/* ========== Chercher la miniature et le titre ========== */

type videoFetcher struct {
	client *http.Client
	mu     sync.Mutex
	busy   map[string]*sync.Mutex
}

// les seuls hôtes contactés, redirections comprises
var videoHosts = []string{"www.youtube.com", "youtube.com", "i.ytimg.com", "vimeo.com", "i.vimeocdn.com",
	"www.dailymotion.com", "dailymotion.com", ".dmcdn.net"}

func videoHostAllowed(u *url.URL) bool {
	if u.Scheme != "https" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, ok := range videoHosts {
		if h == ok || (strings.HasPrefix(ok, ".") && strings.HasSuffix(h, ok)) {
			return true
		}
	}
	return false
}

func newVideoFetcher() *videoFetcher {
	return &videoFetcher{
		busy: map[string]*sync.Mutex{},
		client: &http.Client{
			Timeout: 6 * time.Second,
			CheckRedirect: func(r *http.Request, via []*http.Request) error {
				if len(via) > 3 || !videoHostAllowed(r.URL) {
					return errors.New("redirection refusée")
				}
				return nil
			},
		},
	}
}

func (f *videoFetcher) lock(key string) func() {
	f.mu.Lock()
	m := f.busy[key]
	if m == nil {
		m = &sync.Mutex{}
		f.busy[key] = m
	}
	f.mu.Unlock()
	m.Lock()
	return m.Unlock
}

func (f *videoFetcher) get(raw string, max int64) ([]byte, string, error) {
	u, err := url.Parse(raw)
	if err != nil || !videoHostAllowed(u) {
		return nil, "", errors.New("hôte refusé")
	}
	req, _ := http.NewRequest("GET", u.String(), nil)
	req.Header.Set("User-Agent", "Sloubi.chat (vidéothèque)")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("statut %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > max {
		return nil, "", errors.New("trop gros")
	}
	return b, strings.Split(resp.Header.Get("Content-Type"), ";")[0], nil
}

// fetch va chercher titre, auteur et miniature, et les garde (db/vthumbs, table video_meta).
func (s *server) fetchVideo(provider, id string) {
	defer s.videos.lock(provider + id)()
	var fetched string
	if s.store.DB.QueryRow(`SELECT fetched_at FROM video_meta WHERE provider = ? AND vid = ?`, provider, id).Scan(&fetched) == nil {
		if t, err := time.Parse(tsLayout, fetched); err == nil && time.Since(t) < videoMetaRetry {
			return // déjà fait, ou tenté récemment
		}
	}
	canon := url.QueryEscape(canonicalVideo(provider, id))
	oembed := map[string]string{
		"youtube":     "https://www.youtube.com/oembed?format=json&url=" + canon,
		"vimeo":       "https://vimeo.com/api/oembed.json?url=" + canon,
		"dailymotion": "https://www.dailymotion.com/services/oembed?format=json&url=" + canon,
	}[provider]
	var meta struct {
		Title     string `json:"title"`
		Author    string `json:"author_name"`
		Thumbnail string `json:"thumbnail_url"`
	}
	if b, _, err := s.videos.get(oembed, maxOEmbedBytes); err == nil {
		json.Unmarshal(b, &meta)
	} else {
		log.Printf("vidéothèque %s/%s : %v", provider, id, err)
	}
	thumbURL := meta.Thumbnail
	if provider == "youtube" {
		thumbURL = "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg" // toujours là, même sans oEmbed
	}
	thumb := false
	if thumbURL != "" {
		if b, ct, err := s.videos.get(thumbURL, maxThumbBytes); err == nil && (ct == "image/jpeg" || ct == "image/webp" || ct == "image/png") {
			os.MkdirAll(vthumbRoot, 0o750)
			if os.WriteFile(filepath.Join(vthumbRoot, provider+"-"+id), b, 0o640) == nil {
				os.WriteFile(filepath.Join(vthumbRoot, provider+"-"+id+".type"), []byte(ct), 0o640)
				thumb = true
			}
		}
	}
	s.store.DB.Exec(`INSERT INTO video_meta (provider, vid, title, author, thumb, fetched_at) VALUES (?,?,?,?,?,?)
		ON CONFLICT(provider, vid) DO UPDATE SET title = excluded.title, author = excluded.author, thumb = excluded.thumb, fetched_at = excluded.fetched_at`,
		provider, id, cleanText(meta.Title, false), cleanText(meta.Author, false), thumb, now())
}

// la miniature d'une vidéo citée dans le salon, servie depuis notre cache
func (s *server) videoThumb(c *gin.Context) {
	channel, ok := s.privateRoom(c)
	if !ok {
		return
	}
	provider, id := c.Param("provider"), c.Param("id")
	if (provider != "youtube" && provider != "vimeo" && provider != "dailymotion") || !vidIDRe.MatchString(id) || !s.store.citesVideo(channel, id) {
		c.Status(http.StatusNotFound)
		return
	}
	path := filepath.Join(vthumbRoot, provider+"-"+id)
	if _, err := os.Stat(path); err != nil && s.videos != nil {
		s.fetchVideo(provider, id)
	}
	ct, err := os.ReadFile(path + ".type")
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Content-Type", string(ct))
	c.Header("Cache-Control", "private, max-age=604800")
	c.File(path)
}

// fillVideoMeta va chercher, en parallèle et pas trop longtemps, les titres qui manquent encore.
func (s *server) fillVideoMeta(links []VideoLink) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	n := 0
	for i := range links {
		if links[i].Title != "" || n >= 12 {
			continue
		}
		n++
		wg.Add(1)
		go func(l *VideoLink) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s.fetchVideo(l.Provider, l.ID)
			var title, author sql.NullString
			if s.store.DB.QueryRow(`SELECT title, author FROM video_meta WHERE provider = ? AND vid = ?`, l.Provider, l.ID).Scan(&title, &author) == nil {
				l.Title, l.Author = title.String, author.String
			}
		}(&links[i])
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(4 * time.Second): // la page n'attend pas : les titres en retard viendront à la prochaine visite
	}
}
