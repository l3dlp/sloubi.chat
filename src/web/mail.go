package web

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// Les codes de connexion partent par e-mail, comme sur Daily Jungle : SMTP (Gmail
// avec un mot de passe d'application) s'il est configuré, sinon l'API Brevo, sinon
// le code est écrit dans le journal du service (mode dev : journalctl -u sloubi).

type codeMailer interface {
	SendCode(ctx context.Context, to, code string) error
}

const brevoEndpoint = "https://api.brevo.com/v3/smtp/email"

type mailer struct {
	brevoKey, sender                       string
	smtpHost, smtpPort, smtpUser, smtpPass string
	client                                 *http.Client
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func newMailer() *mailer {
	m := &mailer{
		brevoKey: os.Getenv("BREVO_API_KEY"),
		sender:   envOr("MAIL_SENDER", "no-reply@sloubi.chat"),
		smtpHost: envOr("SMTP_HOST", "smtp.gmail.com"),
		smtpPort: envOr("SMTP_PORT", "587"),
		smtpUser: os.Getenv("SMTP_USER"),
		smtpPass: os.Getenv("SMTP_PASS"),
		client:   &http.Client{Timeout: 20 * time.Second},
	}
	switch {
	case m.smtpUser != "" && m.smtpPass != "":
		log.Printf("mail: SMTP %s", m.smtpHost)
	case m.brevoKey != "":
		log.Printf("mail: Brevo")
	default:
		log.Printf("mail: aucun envoi configuré, les codes de connexion iront dans le journal")
	}
	return m
}

func (m *mailer) SendCode(ctx context.Context, to, code string) error {
	if m.smtpUser != "" && m.smtpPass != "" {
		return m.sendSMTP(to, code)
	}
	if m.brevoKey == "" {
		log.Printf("[mail dev] code de connexion pour %s : %s", to, code)
		return nil
	}
	payload, err := json.Marshal(map[string]any{
		"sender":      map[string]string{"name": "Sloubi.chat", "email": m.sender},
		"to":          []map[string]string{{"email": to}},
		"subject":     codeSubject,
		"htmlContent": codeHTML(code),
		"textContent": codeText(code),
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, brevoEndpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("api-key", m.brevoKey)
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
	return fmt.Errorf("brevo http %d: %s", resp.StatusCode, b)
}

// sendSMTP : STARTTLS puis PLAIN. On s'authentifie avec SMTP_USER et l'on écrit
// From MAIL_SENDER, ce qui marche si c'est un alias « Envoyer en tant que » vérifié.
func (m *mailer) sendSMTP(to, code string) error {
	from := m.sender
	if from == "" {
		from = m.smtpUser
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(m.smtpHost, m.smtpPort), 15*time.Second)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	c, err := smtp.NewClient(conn, m.smtpHost)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: m.smtpHost}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if err := c.Auth(smtp.PlainAuth("", m.smtpUser, m.smtpPass, m.smtpHost)); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	var b strings.Builder
	b.WriteString("From: Sloubi.chat <" + from + ">\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + mimeWord(codeSubject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
	b.WriteString(codeHTML(code))
	if _, err := w.Write([]byte(b.String())); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

const codeSubject = "Ton code Sloubi.chat"

// mimeWord encode un sujet non ASCII (RFC 2047).
func mimeWord(s string) string {
	for _, r := range s {
		if r > 127 {
			return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
		}
	}
	return s
}

func codeText(code string) string {
	return "Ton code de connexion à Sloubi.chat : " + code + "\n\nIl est valable 10 minutes. Si tu n'as rien demandé, ignore ce message."
}

func codeHTML(code string) string {
	return fmt.Sprintf(`<div style="font-family:Georgia,serif;max-width:480px;margin:auto;color:#1f1b16">
<h2 style="letter-spacing:1px">Sloubi.chat</h2>
<p>Ton code de connexion&nbsp;:</p>
<p style="font-size:32px;font-weight:bold;letter-spacing:6px">%s</p>
<p style="color:#666">Il est valable 10&nbsp;minutes. Si tu n'as rien demandé, ignore ce message.</p>
</div>`, code)
}
