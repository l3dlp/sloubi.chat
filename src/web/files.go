package web

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif" // décodeurs pour les miniatures
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sloubi/src/chat"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

/* Fichiers partagés, dans les lieux gardés par un mot de passe. On envoie « à
   l'arrache » : chaque fichier est rangé tout seul (image, vidéo, audio,
   document) d'après son contenu, pas seulement son nom. 10 Go par salon, et
   jamais au point de remplir le disque du serveur.

   Sur le disque : db/uploads/<salon>/<id> (db/ est le seul dossier où le service
   peut écrire), un nom tiré au hasard ; le nom d'origine ne sert qu'à l'affichage.
   Servis seulement à qui a le cookie du salon, jamais interprétés par le
   navigateur (pas de HTML, pas de SVG en ligne). */

const (
	roomQuota    = 10 << 30 // 10 Go par salon
	diskReserve  = 5 << 30  // le disque du serveur garde toujours 5 Go libres
	thumbMax     = 640      // côté le plus long d'une miniature
	thumbMaxPix  = 50e6     // au-delà de 50 Mpx, pas de miniature (mémoire)
	maxNameRunes = 120
)

var uploadRoot = filepath.Join("db", "uploads")

var fileIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

/* ========== Classement ========== */

// ce que le navigateur peut afficher en ligne sans risque ; tout le reste se télécharge
var inlineMime = map[string]string{
	"image/jpeg": "image", "image/png": "image", "image/gif": "image", "image/webp": "image",
	"image/avif": "image", "image/heic": "image", "image/heif": "image", "image/bmp": "image",
	"video/mp4": "video", "video/webm": "video", "video/quicktime": "video", "video/ogg": "video",
	"audio/mpeg": "audio", "audio/mp4": "audio", "audio/aac": "audio", "audio/ogg": "audio",
	"audio/wav": "audio", "audio/wave": "audio", "audio/webm": "audio", "audio/flac": "audio",
}

// les extensions qui disent « c'est un son », même dans un conteneur vidéo
var audioExt = map[string]bool{".weba": true, ".m4a": true, ".opus": true, ".oga": true, ".ogg": true, ".aac": true, ".mp3": true}

var extMime = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp",
	".weba": "audio/webm", ".avif": "image/avif", ".heic": "image/heic", ".heif": "image/heif", ".bmp": "image/bmp",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".mov": "video/quicktime", ".webm": "video/webm", ".ogv": "video/ogg",
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".aac": "audio/aac", ".ogg": "audio/ogg", ".oga": "audio/ogg",
	".opus": "audio/ogg", ".wav": "audio/wav", ".flac": "audio/flac",
	".pdf": "application/pdf", ".zip": "application/zip", ".txt": "text/plain",
}

// classify range un fichier d'après ses premiers octets, puis son extension.
func classify(name string, head []byte) (kind, mime string) {
	sniffed := strings.TrimSpace(strings.Split(http.DetectContentType(head), ";")[0])
	ext := strings.ToLower(filepath.Ext(name))
	// la signature binaire d'abord : un fichier qui commence comme un PNG, un JPEG… en est un, et servi
	// sous ce type il n'exécute rien. (Un vrai PNG peut contenir le texte « <svg » dans ses métadonnées,
	// par exemple un manifeste C2PA : le chercher avant la signature rangeait ces images en documents.)
	if k, ok := inlineMime[sniffed]; ok {
		// un son dans une boîte de vidéo (WebM ou MP4 sans image : messages vocaux, .m4a) reste un son
		if k == "video" && audioExt[ext] {
			if sniffed == "video/webm" {
				return "audio", "audio/webm"
			}
			return "audio", "audio/mp4"
		}
		return k, sniffed
	}
	// du HTML ou du SVG déguisé reste un document, quoi qu'en dise l'extension
	if strings.HasPrefix(sniffed, "text/html") || bytes.Contains(bytes.ToLower(head[:min(len(head), 512)]), []byte("<svg")) {
		return "document", "application/octet-stream"
	}
	if m, ok := extMime[ext]; ok {
		if k, ok := inlineMime[m]; ok {
			// l'extension promet un média : on la croit seulement si le contenu n'est pas reconnu comme autre chose
			if sniffed == "application/octet-stream" || strings.HasPrefix(sniffed, strings.Split(m, "/")[0]+"/") {
				return k, m
			}
		}
		return "document", m
	}
	if sniffed == "application/octet-stream" || sniffed == "" {
		return "document", "application/octet-stream"
	}
	return "document", sniffed
}

// cleanFileName : un nom affichable, sans chemin ni caractères de contrôle.
func cleanFileName(raw string) string {
	n := cleanText(raw, false)
	n = filepath.Base(strings.ReplaceAll(n, "\\", "/"))
	if n == "." || n == "/" || n == "" {
		n = "fichier"
	}
	if utf8.RuneCountInString(n) > maxNameRunes {
		ext := filepath.Ext(n)
		if utf8.RuneCountInString(ext) > 10 {
			ext = ""
		}
		r := []rune(strings.TrimSuffix(n, ext))
		n = string(r[:maxNameRunes-utf8.RuneCountInString(ext)-1]) + "…" + ext
	}
	return n
}

/* ========== Stockage ========== */

func (s *Store) roomUsage(channel string) (int64, error) {
	var n int64
	err := s.DB.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM files WHERE channel = ?`, channel).Scan(&n)
	return n, err
}

func (s *Store) fileCounts(channel string) (map[string]int, error) {
	rows, err := s.DB.Query(`SELECT CASE WHEN kind = 'audio' THEN 'document' ELSE kind END, COUNT(*) FROM files WHERE channel = ? GROUP BY 1`, channel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{"image": 0, "video": 0, "document": 0}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

func (s *Store) fileByID(channel, id string) (*chat.FileRef, error) {
	f := &chat.FileRef{}
	var w, h sql.NullInt64
	err := s.DB.QueryRow(`SELECT id, name, mime, kind, size, w, h, thumb FROM files WHERE id = ? AND channel = ?`, id, channel).
		Scan(&f.ID, &f.Name, &f.Mime, &f.Kind, &f.Size, &w, &h, &f.Thumb)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	f.W, f.H = int(w.Int64), int(h.Int64)
	return f, err
}

// FileItem : une entrée de la médiathèque.
type FileItem struct {
	chat.FileRef
	MessageID int64  `json:"messageId"`
	Nickname  string `json:"nickname"`
	Timestamp string `json:"timestamp"`
}

// listFiles : les fichiers d'un genre, du plus récent au plus ancien ; before = id de message pour paginer.
func (s *Store) listFiles(channel, kind string, before int64, limit int) ([]FileItem, error) {
	kinds := []any{kind}
	where := "f.kind = ?"
	if kind == "document" {
		where = "f.kind IN (?, 'audio')" // les sons sont rangés avec les documents
	}
	q := `SELECT f.id, f.name, f.mime, f.kind, f.size, f.w, f.h, f.thumb, m.id, m.nickname, m.ts
		FROM files f JOIN messages m ON m.file_id = f.id
		WHERE f.channel = ? AND ` + where + ` AND (? = 0 OR m.id < ?) ORDER BY m.id DESC LIMIT ?`
	args := append([]any{channel}, kinds...)
	args = append(args, before, before, limit)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FileItem{}
	for rows.Next() {
		var it FileItem
		var w, h sql.NullInt64
		if err := rows.Scan(&it.ID, &it.Name, &it.Mime, &it.Kind, &it.Size, &w, &h, &it.Thumb, &it.MessageID, &it.Nickname, &it.Timestamp); err != nil {
			return nil, err
		}
		it.W, it.H = int(w.Int64), int(h.Int64)
		out = append(out, it)
	}
	return out, rows.Err()
}

// insertFileMessage enregistre le fichier et le message qui l'annonce, ensemble.
func (s *Store) insertFileMessage(m chat.Message, f chat.FileRef) (chat.Message, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return m, err
	}
	defer tx.Rollback()
	m.TimestampISO = now()
	if _, err := tx.Exec(`INSERT INTO files (id, channel, name, mime, kind, size, w, h, thumb, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		f.ID, m.Channel, f.Name, f.Mime, f.Kind, f.Size, nullInt(f.W), nullInt(f.H), f.Thumb, m.TimestampISO); err != nil {
		return m, err
	}
	res, err := tx.Exec(`INSERT INTO messages (channel, client_id, nickname, message, ts, file_id) VALUES (?,?,?,?,?,?)`,
		m.Channel, m.ClientID, m.Nickname, f.Name, m.TimestampISO, f.ID)
	if err != nil {
		return m, err
	}
	if m.ID, err = res.LastInsertId(); err != nil {
		return m, err
	}
	if _, err := tx.Exec(`UPDATE channels SET last_message_at = ? WHERE name = ?`, m.TimestampISO, m.Channel); err != nil {
		return m, err
	}
	m.Message = f.Name
	m.File = &f
	return m, tx.Commit()
}

func nullInt(n int) any {
	if n <= 0 {
		return nil
	}
	return n
}

func newFileID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func filePath(channel, id string) string { return filepath.Join(uploadRoot, channel, id) }

// diskFree : l'espace libre du disque qui porte db/.
func diskFree() (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs("db", &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

/* ========== Réservations : deux envois simultanés ne dépassent pas le quota ========== */

type reservations struct {
	mu   sync.Mutex
	room map[string]int64
}

func (r *reservations) take(channel string, n int64, used int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.room == nil {
		r.room = map[string]int64{}
	}
	if used+r.room[channel]+n > roomQuota {
		return false
	}
	r.room[channel] += n
	return true
}

func (r *reservations) give(channel string, n int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.room[channel] -= n; r.room[channel] <= 0 {
		delete(r.room, channel)
	}
}

/* ========== Miniatures ========== */

// jpegOrientation lit l'orientation EXIF d'un JPEG (1 à 8), ou 1.
func jpegOrientation(head []byte) int {
	if len(head) < 4 || head[0] != 0xFF || head[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(head) {
		if head[i] != 0xFF {
			return 1
		}
		marker := head[i+1]
		size := int(binary.BigEndian.Uint16(head[i+2:]))
		// size compte ses deux octets : le bloc va de i+2 à i+2+size
		if marker == 0xE1 && size >= 8 && i+2+size <= len(head) && bytes.HasPrefix(head[i+4:], []byte("Exif\x00\x00")) {
			return exifOrientation(head[i+10 : i+2+size])
		}
		if marker == 0xDA { // début de l'image : pas d'EXIF
			return 1
		}
		i += 2 + size
	}
	return 1
}

func exifOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder = binary.BigEndian
	if string(t[:2]) == "II" {
		bo = binary.LittleEndian
	}
	off := int(bo.Uint32(t[4:]))
	if off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off:]))
	for k := 0; k < n; k++ {
		e := off + 2 + k*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
				return v
			}
		}
	}
	return 1
}

// makeThumb : une miniature JPEG, droite (l'orientation EXIF appliquée), par moyenne de pixels.
func makeThumb(src, dst string, orient int) (w, h int, err error) {
	f, err := os.Open(src)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	w, h = cfg.Width, cfg.Height
	if orient >= 5 {
		w, h = h, w
	}
	if float64(cfg.Width)*float64(cfg.Height) > thumbMaxPix {
		return w, h, errors.New("image trop grande pour une miniature")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return w, h, err
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return w, h, err
	}
	b := img.Bounds()
	scale := float64(thumbMax) / float64(max(b.Dx(), b.Dy()))
	if scale > 1 {
		scale = 1
	}
	tw, th := max(1, int(float64(b.Dx())*scale)), max(1, int(float64(b.Dy())*scale))
	small := image.NewRGBA(image.Rect(0, 0, tw, th))
	for y := 0; y < th; y++ {
		y0, y1 := b.Min.Y+y*b.Dy()/th, b.Min.Y+(y+1)*b.Dy()/th
		for x := 0; x < tw; x++ {
			x0, x1 := b.Min.X+x*b.Dx()/tw, b.Min.X+(x+1)*b.Dx()/tw
			var r, g, bl, n uint64
			// on échantillonne au plus 4×4 pixels par case : assez doux, et rapide
			sy, sx := max(1, (y1-y0)/4), max(1, (x1-x0)/4)
			for yy := y0; yy < max(y1, y0+1); yy += sy {
				for xx := x0; xx < max(x1, x0+1); xx += sx {
					// couleurs prémultipliées : on pose la transparence sur du blanc (sinon un PNG détouré devient noir)
					cr, cg, cb, ca := img.At(xx, yy).RGBA()
					white := uint64(0xffff - ca)
					r, g, bl, n = r+uint64(cr)+white, g+uint64(cg)+white, bl+uint64(cb)+white, n+1
				}
			}
			i := small.PixOffset(x, y)
			small.Pix[i], small.Pix[i+1], small.Pix[i+2], small.Pix[i+3] = uint8(r/n>>8), uint8(g/n>>8), uint8(bl/n>>8), 255
		}
	}
	out, err := os.Create(dst)
	if err != nil {
		return w, h, err
	}
	defer out.Close()
	return w, h, jpeg.Encode(out, orient2(small, orient), &jpeg.Options{Quality: 80})
}

// orient2 redresse une image selon l'orientation EXIF.
func orient2(src *image.RGBA, o int) image.Image {
	if o <= 1 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var nx, ny int
			switch o {
			case 2:
				nx, ny = w-1-x, y
			case 3:
				nx, ny = w-1-x, h-1-y
			case 4:
				nx, ny = x, h-1-y
			case 5:
				nx, ny = y, x
			case 6:
				nx, ny = h-1-y, x
			case 7:
				nx, ny = h-1-y, w-1-x
			case 8:
				nx, ny = y, w-1-x
			}
			copy(dst.Pix[dst.PixOffset(nx, ny):dst.PixOffset(nx, ny)+4], src.Pix[src.PixOffset(x, y):src.PixOffset(x, y)+4])
		}
	}
	return dst
}

/* ========== Routes ========== */

// lieu gardé et cookie d'accès : les fichiers ne sortent pas du salon
func (s *server) privateRoom(c *gin.Context) (string, bool) {
	channel, ok := s.paramChannel(c)
	if !ok {
		return "", false
	}
	r, ok := s.enter(c, channel)
	if !ok {
		return "", false
	}
	if !r.Locked() {
		c.JSON(http.StatusForbidden, gin.H{"error": "files need a password"})
		return "", false
	}
	return channel, true
}

// les envois et les téléchargements durent plus que les délais du serveur (15 s / 30 s) : on les prolonge ici
func longRequest(c *gin.Context, d time.Duration) {
	rc := http.NewResponseController(c.Writer)
	if err := rc.SetReadDeadline(time.Now().Add(d)); err != nil {
		log.Printf("files: délai de lecture : %v", err)
	}
	if err := rc.SetWriteDeadline(time.Now().Add(d)); err != nil {
		log.Printf("files: délai d'écriture : %v", err)
	}
}

// uploadFile : le corps de la requête est le fichier, brut (pas de multipart : on écrit au fil de l'eau).
func (s *server) uploadFile(c *gin.Context) {
	channel, ok := s.privateRoom(c)
	if !ok {
		return
	}
	if !s.uploadLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	header := func(k string) string { v, _ := url.QueryUnescape(c.GetHeader(k)); return v }
	nick, reason := s.cleanNickname(header("X-Nickname"))
	if reason != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nickname required", "reason": reason})
		return
	}
	size := c.Request.ContentLength
	if size <= 0 {
		c.JSON(http.StatusLengthRequired, gin.H{"error": "size required"})
		return
	}
	used, err := s.store.roomUsage(channel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if free, err := diskFree(); err != nil || free-size < diskReserve {
		c.JSON(http.StatusInsufficientStorage, gin.H{"error": "server full"})
		return
	}
	if !s.uploads.take(channel, size, used) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "quota", "used": used, "quota": roomQuota})
		return
	}
	defer s.uploads.give(channel, size)

	longRequest(c, 6*time.Hour)
	dir := filepath.Join(uploadRoot, channel)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage"})
		return
	}
	id := newFileID()
	tmp := filepath.Join(dir, ".part-"+id)
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage"})
		return
	}
	defer os.Remove(tmp) // sans effet si on l'a renommé

	// on garde le début pour reconnaître le type (et l'orientation d'une photo)
	var head bytes.Buffer
	body := io.LimitReader(c.Request.Body, size+1)
	n, err := io.Copy(out, io.TeeReader(body, &limitedBuffer{buf: &head, max: 64 << 10}))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil || n != size {
		c.JSON(http.StatusBadRequest, gin.H{"error": "incomplete upload"})
		return
	}

	name := cleanFileName(header("X-File-Name"))
	kind, mime := classify(name, head.Bytes())
	f := chat.FileRef{ID: id, Name: name, Mime: mime, Kind: kind, Size: size}
	final := filePath(channel, id)
	if err := os.Rename(tmp, final); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage"})
		return
	}
	if kind == "image" && (mime == "image/jpeg" || mime == "image/png" || mime == "image/gif") {
		orient := 1
		if mime == "image/jpeg" {
			orient = jpegOrientation(head.Bytes())
		}
		w, h, err := makeThumb(final, final+".thumb.jpg", orient)
		f.W, f.H, f.Thumb = w, h, err == nil
		if err != nil {
			log.Printf("miniature %s: %v", id, err)
		}
	}

	msg, err := s.store.insertFileMessage(chat.Message{Channel: channel, ClientID: cleanClientID(header("X-Client-Id")), Nickname: nick}, f)
	if err != nil {
		os.Remove(final)
		os.Remove(final + ".thumb.jpg")
		log.Printf("upload: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	s.hub.Broadcast(channel, messageJSON(msg))
	c.JSON(http.StatusCreated, gin.H{"id": msg.ID, "file": f})
}

// limitedBuffer garde les premiers octets d'un flux, et ignore la suite.
type limitedBuffer struct {
	buf *bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.buf.Len(); room > 0 {
		l.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// serveFile : en ligne pour les médias sûrs, en téléchargement pour tout le reste.
func (s *server) serveFile(c *gin.Context) {
	channel, ok := s.privateRoom(c)
	if !ok {
		return
	}
	id := c.Param("id")
	if !fileIDRe.MatchString(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	f, err := s.store.fileByID(channel, id)
	if err != nil || f == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	path, mime := filePath(channel, id), f.Mime
	if c.Query("thumb") != "" && f.Thumb {
		path, mime = path+".thumb.jpg", "image/jpeg"
	}
	fh, err := os.Open(path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage"})
		return
	}
	longRequest(c, 6*time.Hour)

	h := c.Writer.Header()
	_, inline := inlineMime[mime]
	if c.Query("dl") != "" || !inline {
		if !inline {
			mime = "application/octet-stream"
		}
		h.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(f.Name))
	} else {
		h.Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(f.Name))
	}
	h.Set("Content-Type", mime)
	h.Set("Cache-Control", "private, max-age=31536000, immutable") // un id ne change jamais de contenu
	// même ouvert seul, le fichier n'exécute rien
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; sandbox")
	http.ServeContent(c.Writer, c.Request, "", st.ModTime(), fh)
}

func (s *server) listFilesRoute(c *gin.Context) {
	channel, ok := s.privateRoom(c)
	if !ok {
		return
	}
	kind := c.Query("kind")
	if kind != "image" && kind != "video" && kind != "document" && kind != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "kind"})
		return
	}
	before, _ := strconv.ParseInt(c.Query("before"), 10, 64)
	used, err := s.store.roomUsage(channel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	counts, err := s.store.fileCounts(channel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	// les liens partagés : les vidéos rejoignent la vidéothèque, le reste les documents
	videos, webs, err := s.store.sharedLinks(channel, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	counts["video"] += len(videos)
	counts["document"] += len(webs)
	out := gin.H{"used": used, "quota": roomQuota, "counts": counts}
	if before == 0 {
		switch kind {
		case "video":
			if s.videos != nil {
				s.fillVideoMeta(videos)
			}
			out["links"] = videos
		case "document":
			out["links"] = webs
		}
	}
	if kind != "" {
		items, err := s.store.listFiles(channel, kind, before, 60)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
			return
		}
		out["files"] = items
	}
	c.JSON(http.StatusOK, out)
}

/* ========== Favoris : les messages aimés ========== */

func (s *Store) favorites(channel string, ids []int64, limit int) ([]chat.Message, error) {
	q := `SELECT m.id, m.nickname, m.message, m.ts, COALESCE(l.n, 0), f.id, f.name, f.mime, f.kind, f.size, f.w, f.h, f.thumb
		FROM messages m LEFT JOIN likes l ON l.message_id = m.id LEFT JOIN files f ON f.id = m.file_id
		WHERE m.channel = ? AND `
	args := []any{channel}
	if len(ids) > 0 {
		q += "m.id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ") ORDER BY m.id DESC LIMIT ?"
		for _, id := range ids {
			args = append(args, id)
		}
	} else {
		q += "l.n > 0 ORDER BY l.n DESC, m.id DESC LIMIT ?"
	}
	args = append(args, limit)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []chat.Message{}
	for rows.Next() {
		var m chat.Message
		var fid, fname, fmime, fkind sql.NullString
		var fsize, fw, fh sql.NullInt64
		var fthumb sql.NullBool
		if err := rows.Scan(&m.ID, &m.Nickname, &m.Message, &m.TimestampISO, &m.Likes, &fid, &fname, &fmime, &fkind, &fsize, &fw, &fh, &fthumb); err != nil {
			return nil, err
		}
		if fid.Valid {
			m.File = &chat.FileRef{ID: fid.String, Name: fname.String, Mime: fmime.String, Kind: fkind.String, Size: fsize.Int64, W: int(fw.Int64), H: int(fh.Int64), Thumb: fthumb.Bool}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *server) favoritesRoute(c *gin.Context) {
	channel, ok := s.paramChannel(c)
	if !ok {
		return
	}
	if !s.checkLimit.Allow(clientKey(c)) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
		return
	}
	if _, ok := s.enter(c, channel); !ok {
		return
	}
	var ids []int64
	for _, p := range strings.Split(c.Query("ids"), ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil && id > 0 && len(ids) < 200 {
			ids = append(ids, id)
		}
	}
	if c.Query("ids") != "" && len(ids) == 0 {
		c.JSON(http.StatusOK, gin.H{"messages": []chat.Message{}})
		return
	}
	msgs, err := s.store.favorites(channel, ids, 200)
	if err != nil {
		log.Printf("favoris: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": msgs})
}

/* ========== Rattrapage : des images rangées en documents par une version précédente ========== */

// reclassify relit le début des « documents » et range en image (avec miniature) ceux qui en sont.
// Sans risque à chaque démarrage : seuls les fichiers dont le classement change sont touchés.
func (s *Store) reclassify() {
	rows, err := s.DB.Query(`SELECT id, channel, name FROM files WHERE kind = 'document'`)
	if err != nil {
		log.Printf("reclassement : %v", err)
		return
	}
	type doc struct{ id, channel, name string }
	var docs []doc
	for rows.Next() {
		var d doc
		if rows.Scan(&d.id, &d.channel, &d.name) == nil {
			docs = append(docs, d)
		}
	}
	rows.Close()
	for _, d := range docs {
		path := filePath(d.channel, d.id)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		head := make([]byte, 64<<10)
		n, _ := io.ReadFull(f, head)
		f.Close()
		head = head[:n]
		kind, mime := classify(d.name, head)
		if kind == "document" {
			continue
		}
		var w, h int
		thumb := false
		if kind == "image" && (mime == "image/jpeg" || mime == "image/png" || mime == "image/gif") {
			orient := 1
			if mime == "image/jpeg" {
				orient = jpegOrientation(head)
			}
			var terr error
			w, h, terr = makeThumb(path, path+".thumb.jpg", orient)
			thumb = terr == nil
		}
		if _, err := s.DB.Exec(`UPDATE files SET kind = ?, mime = ?, w = ?, h = ?, thumb = ? WHERE id = ?`,
			kind, mime, nullInt(w), nullInt(h), thumb, d.id); err == nil {
			log.Printf("reclassement : %s (%s) → %s", d.name, d.channel, kind)
		}
	}
}
