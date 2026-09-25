package mail

import (
	"context"
	"errors"
	stdmail "net/mail"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	gomail "github.com/wneessen/go-mail"
)

type SMTPMailer struct {
	client      *gomail.Client
	fromAddress string
	fromName    string
}

// NewSMTPMailerFromSettings constructs a mailer from the encrypted system
// settings projection. Runtime settings are deliberately separate from env
// configuration so a save can replace the active client without restarting.
func NewSMTPMailerFromSettings(settings application.SMTPSettings) (*SMTPMailer, error) {
	return NewSMTPMailerFromSettingsWithTimeout(settings, 10*time.Second)
}

func NewSMTPMailerFromSettingsWithTimeout(settings application.SMTPSettings, timeout time.Duration) (*SMTPMailer, error) {
	if timeout <= 0 {
		return nil, errors.New("invalid SMTP timeout")
	}
	return newSMTPMailer(settings, timeout)
}

func newSMTPMailer(settings application.SMTPSettings, timeout time.Duration) (*SMTPMailer, error) {
	port := settings.Port
	if port < 1 || port > 65535 {
		return nil, errors.New("invalid SMTP port")
	}
	options := []gomail.Option{gomail.WithPort(port), gomail.WithTimeout(timeout)}
	switch settings.Security {
	case "none":
		options = append(options, gomail.WithTLSPolicy(gomail.NoTLS))
	case "starttls":
		options = append(options, gomail.WithTLSPolicy(gomail.TLSMandatory))
	case "tls":
		options = append(options, gomail.WithSSL())
	default:
		return nil, errors.New("unsupported SMTP security mode")
	}
	if settings.Username != "" || settings.Password != "" {
		options = append(options, gomail.WithSMTPAuth(gomail.SMTPAuthPlain), gomail.WithUsername(settings.Username), gomail.WithPassword(settings.Password))
	}
	client, err := gomail.NewClient(settings.Host, options...)
	if err != nil {
		return nil, err
	}
	return &SMTPMailer{client: client, fromAddress: settings.FromAddress, fromName: settings.FromName}, nil
}

func (m *SMTPMailer) Send(ctx context.Context, outgoing application.OutgoingMail) error {
	message := gomail.NewMsg()
	message.FromMailAddress(&stdmail.Address{Name: m.fromName, Address: m.fromAddress})
	message.ToMailAddress(&stdmail.Address{Name: outgoing.Name, Address: outgoing.To})
	message.Subject(outgoing.Subject)
	message.SetMessageIDWithValue(outgoing.MessageID)
	message.SetBodyString(gomail.TypeTextPlain, outgoing.Text)
	message.AddAlternativeString(gomail.TypeTextHTML, outgoing.HTML)
	if err := m.client.DialAndSendWithContext(ctx, message); err != nil {
		return classifySMTPError(err)
	}
	return nil
}

func classifySMTPError(err error) error {
	var sendErr *gomail.SendError
	if errors.As(err, &sendErr) {
		code := sendErr.ErrorCode()
		if code >= 400 && code < 500 {
			return &application.MailDeliveryError{Code: "temporary", Temporary: true}
		}
		if code >= 500 && code < 600 {
			return &application.MailDeliveryError{Code: "permanent", Temporary: false}
		}
	}
	// Connection, context, and protocol failures without a reply code are
	// retried by the bounded outbox policy. The original SMTP text is never
	// retained or returned to an HTTP caller.
	return &application.MailDeliveryError{Code: "temporary", Temporary: true}
}

var _ application.Mailer = (*SMTPMailer)(nil)
