package chat

type Message struct {
	ID           int64  `json:"id"`
	ClientID     string `json:"clientId"`
	Nickname     string `json:"nickname"`
	Message      string `json:"message"`
	SloubiNumber *int   `json:"sloubiNumber,omitempty"` // nil si pas un sloubi
	TimestampISO string `json:"timestamp"`
}
