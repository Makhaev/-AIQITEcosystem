package invitations

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aiq.local/core/internal/platform/config"
)

func TestSMTPDeliveryRequiresVerifiedTLS(t *testing.T) {
	for _, mode := range []string{"starttls", "tls"} {
		t.Run(mode, func(t *testing.T) {
			certServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			defer certServer.Close()
			caFile := filepath.Join(t.TempDir(), "ca.pem")
			if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certServer.Certificate().Raw}), 0600); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			host, port, _ := net.SplitHostPort(listener.Addr().String())
			done := make(chan error, 1)
			go func() {
				conn, e := listener.Accept()
				if e != nil {
					done <- e
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				serverTLS := &tls.Config{Certificates: certServer.TLS.Certificates, MinVersion: tls.VersionTLS12}
				if mode == "tls" {
					conn = tls.Server(conn, serverTLS)
				}
				wire := textproto.NewConn(conn)
				if e = wire.PrintfLine("220 localhost ESMTP"); e != nil {
					done <- e
					return
				}
				command := func(prefix, reply string) error {
					line, e := wire.ReadLine()
					if e != nil {
						return e
					}
					if !strings.HasPrefix(line, prefix) {
						return fmt.Errorf("unexpected SMTP command for %s", prefix)
					}
					return wire.PrintfLine("%s", reply)
				}
				if mode == "starttls" {
					if e = command("EHLO ", "250-localhost\r\n250 STARTTLS"); e != nil {
						done <- e
						return
					}
					if e = command("STARTTLS", "220 Start encryption"); e != nil {
						done <- e
						return
					}
					conn = tls.Server(conn, serverTLS)
					wire = textproto.NewConn(conn)
				}
				if e = command("EHLO ", "250-localhost\r\n250 AUTH PLAIN"); e != nil {
					done <- e
					return
				}
				if e = command("AUTH PLAIN ", "235 Authenticated"); e != nil {
					done <- e
					return
				}
				if e = command("MAIL FROM:<invites@example.test>", "250 OK"); e != nil {
					done <- e
					return
				}
				if e = command("RCPT TO:<recipient@example.test>", "250 OK"); e != nil {
					done <- e
					return
				}
				if e = command("DATA", "354 Send content"); e != nil {
					done <- e
					return
				}
				body, e := wire.ReadDotBytes()
				if e != nil {
					done <- e
					return
				}
				if !strings.Contains(string(body), "/#/invitation?token=synthetic-token") || !strings.Contains(string(body), "48 часов") {
					done <- fmt.Errorf("invitation email missing link or expiry")
					return
				}
				if e = wire.PrintfLine("250 Queued"); e != nil {
					done <- e
					return
				}
				done <- command("QUIT", "221 Goodbye")
			}()
			mailer := &SMTPMailer{Config: config.SMTPConfig{Host: host, Port: port, TLSMode: mode, From: "invites@example.test", Username: "synthetic-user", Password: "synthetic-password", CAFile: caFile}}
			if err = mailer.Send(context.Background(), "recipient@example.test", "https://aiq.example.test/#/invitation?token=synthetic-token"); err != nil {
				t.Fatal(err)
			}
			if err = <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSMTPConfigurationRejectsMissingAndPlaintextSettings(t *testing.T) {
	for _, cfg := range []config.SMTPConfig{{}, {Host: "smtp.example.test", From: "invites@example.test", TLSMode: "none"}, {Host: "smtp.example.test", From: "injected\r\n@example.test"}, {Host: "smtp.example.test", From: "invites@example.test", Port: "-1"}, {Host: "smtp.example.test", From: "invites@example.test", Username: "user"}} {
		if (&SMTPMailer{Config: cfg}).Ready() == nil {
			t.Fatal("invalid SMTP settings accepted")
		}
	}
}

func TestSMTPInvalidCAFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.pem")
	for _, present := range []bool{false, true} {
		if present {
			if err := os.WriteFile(path, []byte("not a certificate"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		mailer := &SMTPMailer{Config: config.SMTPConfig{CAFile: path}}
		if _, err := mailer.tlsConfig(); err == nil {
			t.Fatal("invalid CA accepted")
		}
	}
}
