package mail

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
)

func TestNewSMTPMailerSupportsConfiguredTransportModes(t *testing.T) {
	for _, mode := range []string{"none", "starttls", "tls"} {
		t.Run(mode, func(t *testing.T) {
			mailer, err := NewSMTPMailerFromSettingsWithTimeout(application.SMTPSettings{
				Host: "127.0.0.1", Port: 2525, Security: mode,
				FromAddress: "no-reply@example.com", FromName: "Temvia",
			}, time.Second)
			if err != nil || mailer == nil {
				t.Fatalf("NewSMTPMailerFromSettings(%q) = %#v, %v", mode, mailer, err)
			}
		})
	}
	if _, err := NewSMTPMailerFromSettingsWithTimeout(application.SMTPSettings{Host: "127.0.0.1", Port: 2525, Security: "invalid"}, time.Second); err == nil {
		t.Fatal("invalid SMTP transport mode accepted")
	}
}

func TestClassifySMTPErrorRedactsProtocolDetails(t *testing.T) {
	err := classifySMTPError(errors.New("535 5.7.8 authentication secret@example.com failed"))
	var delivery *application.MailDeliveryError
	if !errors.As(err, &delivery) || !delivery.Temporary || delivery.Code != "temporary" {
		t.Fatalf("classified error = %#v, %v", delivery, err)
	}
	if errors.Is(err, context.Canceled) || err.Error() != "mail delivery temporary" {
		t.Fatalf("classified error leaked details: %v", err)
	}
}
