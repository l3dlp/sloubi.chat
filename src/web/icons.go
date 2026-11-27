package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

/* ========== L'application d'un domaine perso ==========

Sur son domaine, un lieu s'installe comme sa propre application : le manifeste
porte son nom (« Tribu » pour /tribu) et ses icônes sont l'initiale du nom, dans
les couleurs de son décor. Les icônes sont dessinées ici (police Go Bold, BSD)
et gardées en mémoire.
*/

// accent : la couleur de l'initiale, celle du décor (--ember dans themes.css).
var accent = map[string]string{
	"taverne": "#ff8a3d",
	"foret":   "#6ee0a6",
	"moulin":  "#7cc8e4",
	"cyber":   "#00f0ff",
	"kawaii":  "#9b7bff",
	"bd":      "#3e6990",
}

// appName : « tribu » → « Tribu », « mon-chan.v2 » → « Mon chan v2 ».
func appName(channel string) string {
	n := strings.NewReplacer("-", " ", "_", " ", ".", " ").Replace(channel)
	r, size := utf8.DecodeRuneInString(n)
	return string(unicode.ToUpper(r)) + n[size:]
}

func hexColor(h string) color.RGBA {
	var c color.RGBA
	c.A = 255
	if len(h) == 7 && h[0] == '#' {
		v := func(i int) uint8 {
			var x uint8
			for _, ch := range h[i : i+2] {
				x <<= 4
				switch {
				case ch >= '0' && ch <= '9':
					x |= uint8(ch - '0')
				case ch >= 'a' && ch <= 'f':
					x |= uint8(ch-'a') + 10
				case ch >= 'A' && ch <= 'F':
					x |= uint8(ch-'A') + 10
				}
			}
			return x
		}
		c.R, c.G, c.B = v(1), v(3), v(5)
	}
	return c
}

var (
	iconFontOnce sync.Once
	iconFont     *opentype.Font
	iconMu       sync.Mutex
	iconCache    = map[string][]byte{} // lieu|décor|taille → PNG
)

// roomIcon : un carré plein bord (« maskable ») aux couleurs du décor, l'initiale au centre.
func roomIcon(channel, theme string, size int) ([]byte, error) {
	key := fmt.Sprintf("%s|%s|%d", channel, theme, size)
	iconMu.Lock()
	if b, ok := iconCache[key]; ok {
		iconMu.Unlock()
		return b, nil
	}
	iconMu.Unlock()

	var err error
	iconFontOnce.Do(func() { iconFont, err = opentype.Parse(gobold.TTF) })
	if err != nil || iconFont == nil {
		return nil, err
	}
	bg, fg := hexColor(themeOf(theme).Color), hexColor(accent[themeOf(theme).ID])
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	r, _ := utf8.DecodeRuneInString(channel)
	letter := string(unicode.ToUpper(r))
	face, err := opentype.NewFace(iconFont, &opentype.FaceOptions{Size: float64(size) * 0.66, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, err
	}
	defer face.Close()
	d := &font.Drawer{Dst: img, Src: &image.Uniform{fg}, Face: face}
	bounds, _ := d.BoundString(letter)
	// centré sur la boîte de la lettre, pas sur la ligne de base
	w, h := bounds.Max.X-bounds.Min.X, bounds.Max.Y-bounds.Min.Y
	half := fixed.I(size / 2)
	d.Dot = fixed.Point26_6{X: half - w/2 - bounds.Min.X, Y: half - h/2 - bounds.Min.Y}
	d.DrawString(letter)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	iconMu.Lock()
	if len(iconCache) > 500 { // garde-fou : on repart de zéro plutôt que de grossir sans fin
		iconCache = map[string][]byte{}
	}
	iconCache[key] = buf.Bytes()
	iconMu.Unlock()
	return buf.Bytes(), nil
}

// serveRoomApp : manifeste et icônes d'un lieu sur son domaine. Renvoie faux si le chemin n'en est pas.
func (s *server) serveRoomApp(c *gin.Context, channel string) bool {
	sizes := map[string]int{"/icon-192.png": 192, "/icon-512.png": 512, "/apple-touch-icon.png": 180, "/favicon.ico": 64, "/favicon.png": 64}
	p := c.Request.URL.Path
	size, isIcon := sizes[p]
	if !isIcon && p != "/manifest.json" {
		return false
	}
	r, err := s.store.room(channel)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return true
	}
	th := themeOf(r.Theme)
	c.Header("Cache-Control", "public, max-age=3600") // un changement de décor se voit dans l'heure
	if isIcon {
		b, err := roomIcon(channel, th.ID, size)
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return true
		}
		c.Data(http.StatusOK, "image/png", b)
		return true
	}
	name := appName(channel)
	manifest, _ := json.Marshal(gin.H{
		"id": "/", "name": name, "short_name": name, "description": th.Description,
		"start_url": "/", "scope": "/", "display": "standalone", "orientation": "any",
		"background_color": th.Color, "theme_color": th.Color, "lang": "fr",
		"icons": []gin.H{
			{"src": "/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any maskable"},
			{"src": "/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any maskable"},
		},
	})
	c.Data(http.StatusOK, "application/manifest+json", manifest)
	return true
}
