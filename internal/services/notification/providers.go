package notification

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"sing-box-web-panel/internal/domain"
)

type SMTPClient struct{}

func NewSMTPClient() *SMTPClient { return &SMTPClient{} }

func (c *SMTPClient) Send(ctx context.Context, settings domain.NotificationSettings, message Message) error {
	timeout := time.Duration(settings.SMTPTimeoutSec) * time.Second
	address := net.JoinHostPort(settings.SMTPHost, fmt.Sprintf("%d", settings.SMTPPort))
	dialer := &net.Dialer{Timeout: timeout}
	var conn net.Conn
	var err error
	tlsConfig := &tls.Config{ServerName: settings.SMTPHost, MinVersion: tls.VersionTLS12}
	if settings.SMTPMode == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("connect SMTP server: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	client := textproto.NewConn(conn)
	if _, _, err := client.ReadResponse(220); err != nil {
		return fmt.Errorf("read SMTP greeting: %w", err)
	}
	if err := smtpCommand(client, 250, "EHLO localhost"); err != nil {
		return fmt.Errorf("SMTP EHLO: %w", err)
	}
	if settings.SMTPMode == "starttls" {
		if err := smtpCommand(client, 220, "STARTTLS"); err != nil {
			return fmt.Errorf("SMTP STARTTLS: %w", err)
		}
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
		conn = tlsConn
		client = textproto.NewConn(conn)
		if err := smtpCommand(client, 250, "EHLO localhost"); err != nil {
			return fmt.Errorf("SMTP EHLO after TLS: %w", err)
		}
	}
	if settings.SMTPUsername != "" {
		credentials := base64.StdEncoding.EncodeToString([]byte("\x00" + settings.SMTPUsername + "\x00" + settings.SMTPPassword))
		if err := smtpCommand(client, 235, "AUTH PLAIN %s", credentials); err != nil {
			return fmt.Errorf("authenticate SMTP: %w", err)
		}
	}
	from, _ := parseMailbox(settings.SMTPFrom)
	if err := smtpCommand(client, 250, "MAIL FROM:<%s>", from); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	for _, raw := range settings.SMTPRecipients {
		address, _ := parseMailbox(raw)
		if err := smtpCommand(client, 250, "RCPT TO:<%s>", address); err != nil {
			return fmt.Errorf("set SMTP recipient: %w", err)
		}
	}
	if err := smtpCommand(client, 354, "DATA"); err != nil {
		return fmt.Errorf("open SMTP message: %w", err)
	}
	w := client.DotWriter()
	var body bytes.Buffer
	writer := bufio.NewWriter(&body)
	fmt.Fprintf(writer, "From: %s\r\n", settings.SMTPFrom)
	fmt.Fprintf(writer, "To: %s\r\n", strings.Join(settings.SMTPRecipients, ", "))
	fmt.Fprintf(writer, "Subject: %s\r\n", sanitizeHeader(message.Subject))
	fmt.Fprint(writer, "MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n")
	fmt.Fprint(writer, message.Body)
	writer.Flush()
	if _, err := io.Copy(w, &body); err != nil {
		w.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	if _, _, err := client.ReadResponse(250); err != nil {
		return fmt.Errorf("accept SMTP message: %w", err)
	}
	if err := smtpCommand(client, 221, "QUIT"); err != nil {
		return fmt.Errorf("quit SMTP: %w", err)
	}
	return nil
}

func smtpCommand(client *textproto.Conn, expect int, format string, args ...any) error {
	if err := client.PrintfLine(format, args...); err != nil {
		return err
	}
	_, _, err := client.ReadResponse(expect)
	return err
}

func parseMailbox(raw string) (string, error) {
	address, err := mailParseAddress(raw)
	if err != nil {
		return "", err
	}
	return address, nil
}

var mailParseAddress = func(raw string) (string, error) {
	parsed, err := mail.ParseAddress(raw)
	if err != nil {
		return "", err
	}
	return parsed.Address, nil
}

func sanitizeHeader(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}

type TelegramClient struct{}

func NewTelegramClient() *TelegramClient { return &TelegramClient{} }

func (c *TelegramClient) Send(ctx context.Context, settings domain.NotificationSettings, message Message) error {
	base, err := url.Parse(settings.TelegramAPIBase)
	if err != nil {
		return fmt.Errorf("invalid Telegram API base")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/bot" + settings.TelegramBotToken + "/sendMessage"
	payload, _ := json.Marshal(map[string]string{"chat_id": settings.TelegramChatID, "text": message.Subject + "\n" + message.Body})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build Telegram request")
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: time.Duration(settings.TelegramTimeoutSec) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send Telegram request: %w", err)
	}
	defer response.Body.Close()
	limited, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Telegram API returned %s: %s", response.Status, strings.TrimSpace(string(limited)))
	}
	return nil
}
