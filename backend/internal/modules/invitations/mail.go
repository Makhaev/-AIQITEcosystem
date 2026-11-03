package invitations

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"

	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/config"
	"aiq.local/core/internal/platform/httpx"
)

type SMTPMailer struct{ Config config.SMTPConfig }

func (m *SMTPMailer) settings() config.SMTPConfig {
	c := m.Config
	if c.TLSMode == "" {
		c.TLSMode = "starttls"
	}
	if c.Port == "" {
		c.Port = "587"
		if c.TLSMode == "tls" {
			c.Port = "465"
		}
	}
	return c
}
func (m *SMTPMailer) Ready() error {
	c := m.settings()
	port, e := strconv.Atoi(c.Port)
	_, emailErr := users.NormalizeEmail(c.From)
	if c.Host == "" || strings.ContainsAny(c.Host, "\r\n") || e != nil || port < 1 || port > 65535 || emailErr != nil || (c.TLSMode != "starttls" && c.TLSMode != "tls") || (c.Username == "") != (c.Password == "") {
		return httpx.E(503, "mail_unavailable", "Отправка приглашений не настроена. Проверьте SMTP_HOST, SMTP_PORT, SMTP_FROM и параметры авторизации/TLS.")
	}
	return nil
}
func (m *SMTPMailer) Send(ctx context.Context, email, link string) error {
	cfg, err := m.tlsConfig()
	if err != nil {
		return err
	}
	return m.send(ctx, email, link, cfg)
}

// An explicit CA is useful for a private relay or a local pilot. It never
// disables certificate, hostname or expiration verification.
func (m *SMTPMailer) tlsConfig() (*tls.Config, error) {
	cfg := &tls.Config{ServerName: m.Config.Host, MinVersion: tls.VersionTLS12}
	if m.Config.CAFile == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(m.Config.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read SMTP CA: %w", err)
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("SMTP_CA_FILE contains no certificates")
	}
	cfg.RootCAs = roots
	return cfg, nil
}

func (m *SMTPMailer) send(ctx context.Context, email, link string, tlsConfig *tls.Config) error {
	return m.sendMessage(ctx, email, "Приглашение в AIQ Core", "Вас пригласили в AIQ Core.\r\n\r\nОткройте ссылку, чтобы задать пароль или войти в существующий аккаунт:\r\n"+link+"\r\n\r\nСсылка действует 48 часов с момента создания и может быть использована один раз. При повторной отправке прежняя ссылка перестаёт действовать.\r\nЕсли приглашение предназначено не вам, не используйте его.\r\n", tlsConfig)
}

func (m *SMTPMailer) SendPasswordReset(ctx context.Context, email, link string) error {
	cfg, err := m.tlsConfig()
	if err != nil {
		return err
	}
	return m.sendMessage(ctx, email, "Восстановление пароля AIQ Core", "Для установки нового пароля откройте ссылку:\r\n"+link+"\r\n\r\nСсылка действует 30 минут и может быть использована один раз. После смены пароля все сессии аккаунта завершатся.\r\nЕсли вы не запрашивали восстановление, проигнорируйте письмо.\r\n", cfg)
}

func (m *SMTPMailer) SendNotification(ctx context.Context, email, subject, link string) error {
	if _, err := users.NormalizeEmail(email); err != nil {
		return err
	}
	cfg, err := m.tlsConfig()
	if err != nil {
		return err
	}
	return m.sendMessage(ctx, email, subject+" — AIQ Core", "В вашем кабинете новое уведомление. Подробности доступны после входа:\r\n"+link+"\r\n", cfg)
}

func (m *SMTPMailer) sendMessage(ctx context.Context, email, subject, body string, tlsConfig *tls.Config) error {
	if err := m.Ready(); err != nil {
		return err
	}
	c := m.settings()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(c.Host, c.Port))
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return err
	}
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	defer stop()
	if c.TLSMode == "tls" {
		secure := tls.Client(conn, tlsConfig)
		if err = secure.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = secure
	}
	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if c.TLSMode == "starttls" {
		if err = client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if c.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return err
		}
	}
	from, _ := users.NormalizeEmail(c.From)
	if err = client.Mail(from); err != nil {
		return err
	}
	if err = client.Rcpt(email); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	message := "From: " + from + "\r\nTo: " + email + "\r\nSubject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" + body
	if _, err = fmt.Fprint(w, message); err != nil {
		_ = w.Close()
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	// A positive DATA response confirms handoff. A failed QUIT does not undo it.
	_ = client.Quit()
	return nil
}
