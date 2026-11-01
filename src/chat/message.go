package chat

type Message struct {
	ID           int64  `json:"id"`
	Channel      string `json:"channel"`
	ClientID     string `json:"-"` // jamais diffusé : sinon n'importe qui peut se faire passer pour un autre
	Nickname     string `json:"nickname"`
	Message      string `json:"message"`
	SloubiNumber *int   `json:"sloubiNumber,omitempty"` // nil si pas un sloubi
	TimestampISO string `json:"timestamp"`
}

type Postit struct {
	Channel   string `json:"channel"`
	Text      string `json:"text"`
	UpdatedAt string `json:"updatedAt"`
}
