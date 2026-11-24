package auth

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"qauipsiz/internal/domain"
)

type memRepo struct {
	credential Credential
	sessions   map[string]domain.User
	touched    int
}

func newRepo(t *testing.T) *memRepo {
	t.Helper()
	h, err := HashPassword("Start123")
	if err != nil {
		t.Fatal(err)
	}
	return &memRepo{credential: Credential{User: domain.User{ID: 1, IIN: "900101300017", Role: domain.RoleHSE, Active: true}, PasswordHash: h}, sessions: map[string]domain.User{}}
}

func (m *memRepo) LoginCredential(_ context.Context, iin string) (Credential, error) {
	if iin != m.credential.User.IIN {
		return Credential{}, domain.ErrNotFound
	}
	return m.credential, nil
}
func (m *memRepo) CreateSession(_ context.Context, id int64, token string, _ time.Time) (domain.User, error) {
	if id != m.credential.User.ID {
		return domain.User{}, domain.ErrNotFound
	}
	m.sessions[token] = m.credential.User
	return m.credential.User, nil
}
func (m *memRepo) SessionUser(_ context.Context, token string, _ time.Time) (domain.User, error) {
	u, ok := m.sessions[token]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}
func (m *memRepo) DeleteSession(_ context.Context, token string) error {
	delete(m.sessions, token)
	return nil
}
func (m *memRepo) PasswordHash(_ context.Context, id int64) (string, error) {
	if id != 1 {
		return "", domain.ErrNotFound
	}
	return m.credential.PasswordHash, nil
}
func (m *memRepo) UpdatePassword(_ context.Context, id int64, h string) error {
	m.credential.PasswordHash = h
	return nil
}
func (m *memRepo) TouchLastSeen(_ context.Context, _ int64, _ time.Time) error {
	m.touched++
	return nil
}

func invalid(err error, part string) bool {
	var ve domain.ValidationError
	return errors.As(err, &ve) && strings.Contains(string(ve), part)
}

func TestLoginByIINAndSession(t *testing.T) {
	repo := newRepo(t)
	s := New(repo)
	if _, _, err := s.Login(context.Background(), "900101300017", "wrong"); !invalid(err, "ИИН или пароль") {
		t.Fatalf("wrong password: %v", err)
	}
	if _, _, err := s.Login(context.Background(), "123", "Start123"); !invalid(err, "ИИН или пароль") {
		t.Fatalf("bad IIN: %v", err)
	}
	token, u, err := s.Login(context.Background(), "900101300017", "Start123")
	if err != nil || token == "" || u.ID != 1 {
		t.Fatalf("login: %v", err)
	}
	if got, err := s.SessionUser(context.Background(), token); err != nil || got.ID != 1 {
		t.Fatalf("session: %v", err)
	}
	if err := s.Logout(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(context.Background(), token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("session survived logout")
	}
}

func TestChangePassword(t *testing.T) {
	repo := newRepo(t)
	s := New(repo)
	u := repo.credential.User
	if err := s.ChangePassword(context.Background(), u, "bad", "NewPass99", "NewPass99"); !invalid(err, "Текущий") {
		t.Fatalf("current: %v", err)
	}
	if err := s.ChangePassword(context.Background(), u, "Start123", "short", "short"); !invalid(err, "8") {
		t.Fatalf("short: %v", err)
	}
	if err := s.ChangePassword(context.Background(), u, "Start123", "NewPass99", "different"); !invalid(err, "не совпадают") {
		t.Fatalf("confirm: %v", err)
	}
	if err := s.ChangePassword(context.Background(), u, "Start123", "NewPass99", "NewPass99"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Login(context.Background(), u.IIN, "NewPass99"); err != nil {
		t.Fatalf("new password: %v", err)
	}
}

func TestInitialPasswordFormat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		p, err := NewInitialPassword()
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^[A-Za-z0-9]{8}$`).MatchString(p) || !regexp.MustCompile(`[A-Za-z]`).MatchString(p) || !regexp.MustCompile(`[0-9]`).MatchString(p) {
			t.Fatalf("bad password %q", p)
		}
		seen[p] = true
	}
	if len(seen) < 99 {
		t.Fatalf("passwords are not random enough: %d unique", len(seen))
	}
}

func TestTouchLastSeenAtMostEveryFiveMinutes(t *testing.T) {
	repo := newRepo(t)
	now := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	s := New(repo).WithClock(func() time.Time { return now })
	if err := s.TouchLastSeen(context.Background(), domain.User{ID: 1}); err != nil || repo.touched != 1 {
		t.Fatal(err)
	}
	recent := now.Add(-time.Minute)
	if err := s.TouchLastSeen(context.Background(), domain.User{ID: 1, LastSeenAt: &recent}); err != nil || repo.touched != 1 {
		t.Fatal("recent touch should be skipped")
	}
}
