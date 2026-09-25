package application

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type mailTaskTestPrincipalStore struct {
	principal domain.Principal
}

func (s mailTaskTestPrincipalStore) FindPrincipalByID(context.Context, string) (domain.Principal, error) {
	return s.principal, nil
}

type mailTaskTestStore struct {
	retryErr  error
	deleteErr error
	retried   []string
	deleted   []string
}

func (s *mailTaskTestStore) ListMailTasks(context.Context, MailTaskListOptions) (MailTaskPage, error) {
	return MailTaskPage{}, nil
}
func (s *mailTaskTestStore) FindMailTask(context.Context, string) (MailTask, error) {
	return MailTask{}, nil
}
func (s *mailTaskTestStore) RetryMailTask(_ context.Context, id string) (MailTask, error) {
	s.retried = append(s.retried, id)
	return MailTask{ID: id}, s.retryErr
}
func (s *mailTaskTestStore) DeleteMailTask(_ context.Context, id string) error {
	s.deleted = append(s.deleted, id)
	return s.deleteErr
}
func (s *mailTaskTestStore) EnqueueMailTask(context.Context, MailTaskInput) (MailTask, error) {
	return MailTask{}, nil
}

func mailTaskTestPrincipal(permissions ...domain.PermissionKey) domain.Principal {
	return domain.Principal{
		User:  domain.User{ID: "00000000-0000-4000-8000-000000000001"},
		Roles: []domain.Role{{ID: "00000000-0000-4000-8000-000000000002", Name: "mail operator", Permissions: permissions}},
	}
}

func TestMailTaskMaterialIsPurposeSeparatedAndAuthenticated(t *testing.T) {
	master := bytes.Repeat([]byte{0x4a}, 32)
	box, err := NewMailTaskSecretBox(master)
	if err != nil {
		t.Fatal(err)
	}
	material := MailTaskMaterial{
		Version:    1,
		Kind:       MailTest,
		Email:      "operator@example.com",
		Locale:     domain.LocaleEnglish,
		SystemName: "Temvia",
		CreatedAt:  time.Now().UTC(),
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		Message:    &OutgoingMail{Kind: MailTest, To: "operator@example.com", Locale: domain.LocaleEnglish},
	}
	ciphertext, err := SealMailTaskMaterial(box, material)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte(material.Email)) || bytes.Contains(ciphertext, []byte(material.SystemName)) {
		t.Fatal("mail-task material was persisted in plaintext")
	}
	opened, err := OpenMailTaskMaterial(box, ciphertext)
	if err != nil || opened.Email != material.Email || opened.Kind != material.Kind {
		t.Fatalf("OpenMailTaskMaterial() = %#v, %v", opened, err)
	}
	ciphertext[len(ciphertext)-1] ^= 1
	if _, err := OpenMailTaskMaterial(box, ciphertext); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("tampered material error = %v", err)
	}
}

func TestMailTaskManagementKeepsReadAndWriteIndependent(t *testing.T) {
	actorID := "00000000-0000-4000-8000-000000000001"
	taskID := "00000000-0000-4000-8000-000000000003"
	store := &mailTaskTestStore{}
	catalog, err := domain.NewPermissionCatalog(domain.DefaultPermissionCatalog().Definitions()...)
	if err != nil {
		t.Fatal(err)
	}
	readOnly := NewMailTaskManagement(store, mailTaskTestPrincipalStore{principal: mailTaskTestPrincipal(domain.PermissionMailTasksRead)}, catalog)
	if _, err := readOnly.Retry(context.Background(), actorID, taskID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read-only retry error = %v", err)
	}
	if _, err := readOnly.Detail(context.Background(), actorID, taskID); err != nil {
		t.Fatalf("read-only detail error = %v", err)
	}
	writeOnly := NewMailTaskManagement(store, mailTaskTestPrincipalStore{principal: mailTaskTestPrincipal(domain.PermissionMailTasksWrite)}, catalog)
	if _, err := writeOnly.List(context.Background(), actorID, MailTaskListOptions{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("write-only list error = %v", err)
	}
	if _, err := writeOnly.Retry(context.Background(), actorID, taskID); err != nil {
		t.Fatalf("write-only retry error = %v", err)
	}
	if len(store.retried) != 1 || store.retried[0] != taskID {
		t.Fatalf("retry calls = %#v", store.retried)
	}
}
