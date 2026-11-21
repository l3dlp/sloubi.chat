package chat

type Message struct {
	ID           int64  `json:"id"`
	Channel      string `json:"channel"`
	ClientID     string `json:"-"` // jamais diffusé : sinon n'importe qui peut se faire passer pour un autre
	Nickname     string `json:"nickname"`
	Message      string `json:"message"`
	SloubiNumber *int   `json:"sloubiNumber,omitempty"` // nil si pas un sloubi
	TimestampISO string `json:"timestamp"`

	// des totaux, rien d'autre : on ne retient pas qui a aimé ni qui a cliqué
	Likes  int            `json:"likes,omitempty"`
	Clicks map[string]int `json:"clicks,omitempty"` // clics par lien, par ordre d'apparition dans le message ("0", "1"…)

	File *FileRef `json:"file,omitempty"` // un message peut être un fichier partagé
}

// FileRef : un fichier partagé dans un lieu gardé. Kind : image, video, audio ou document.
type FileRef struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Mime  string `json:"mime"`
	Kind  string `json:"kind"`
	Size  int64  `json:"size"`
	W     int    `json:"w,omitempty"`
	H     int    `json:"h,omitempty"`
	Thumb bool   `json:"thumb,omitempty"`
}

type Postit struct {
	Channel   string `json:"channel"`
	Text      string `json:"text"`
	UpdatedAt string `json:"updatedAt"`
}
