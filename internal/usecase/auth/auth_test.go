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
	users    map[string]domain.User
	codes    []*memCode
	sessions map[string]domain.User
}

type memCode struct {
	Code
	phone   string
	expires time.Time
	created time.Time
	used    bool
}

func newRepo() *memRepo {
	return &memRepo{users: map[string]domain.User{"77010000001": {ID: 1, Role: domain.RoleHSE, Active: true}}, sessions: map[string]domain.User{}}
}

func (m *memRepo) ActiveUserExists(_ context.Context, phone string) (bool, error) {
	_, ok := m.users[phone]
	return ok, nil
}
func (m *memRepo) LastCodeAt(_ context.Context, phone string) (*time.Time, error) {
	var last *time.Time
	for _, c := range m.codes {
		if c.phone == phone {
			t := c.created
			last = &t
		}
	}
	return last, nil
}
func (m *memRepo) CodesSince(_ context.Context, phone string, since time.Time) (int, error) {
	n := 0
	for _, c := range m.codes {
		if c.phone == phone && !c.created.Before(since) {
			n++
		}
	}
	return n, nil
}
func (m *memRepo) SaveCode(_ context.Context, phone, h string, exp time.Time) error {
	m.codes = append(m.codes, &memCode{Code: Code{ID: int64(len(m.codes) + 1), Hash: h}, phone: phone, expires: exp, created: exp.Add(-CodeTTL)})
	return nil
}
func (m *memRepo) ActiveCode(_ context.Context, phone string, now time.Time) (Code, error) {
	for i := len(m.codes) - 1; i >= 0; i-- {
		c := m.codes[i]
		if c.phone == phone && !c.used && c.expires.After(now) {
			return c.Code, nil
		}
	}
	return Code{}, domain.ErrNotFound
}
func (m *memRepo) IncAttempts(_ context.Context, id int64) error {
	m.codes[id-1].Attempts++
	return nil
}
func (m *memRepo) UseCode(_ context.Context, id int64) (bool, error) {
	if m.codes[id-1].used {
		return false, nil
	}
	m.codes[id-1].used = true
	return true, nil
}
func (m *memRepo) CreateSession(_ context.Context, phone, th string, _ time.Time) (domain.User, error) {
	m.sessions[th] = m.users[phone]
	return m.users[phone], nil
}
func (m *memRepo) SessionUser(_ context.Context, th string, _ time.Time) (domain.User, error) {
	if u, ok := m.sessions[th]; ok {
		return u, nil
	}
	return domain.User{}, domain.ErrNotFound
}
func (m *memRepo) DeleteSession(_ context.Context, th string) error {
	delete(m.sessions, th)
	return nil
}

type memSMS struct{ last string }

func (s *memSMS) Send(_, text string) error { s.last = text; return nil }
func (s *memSMS) code() string              { return regexp.MustCompile(`\d{4}$`).FindString(s.last) }

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func setup(fixed string) (*Service, *memRepo, *memSMS, *clock) {
	repo, sms, clk := newRepo(), &memSMS{}, &clock{time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)}
	return New(repo, sms, fixed).WithClock(clk.now), repo, sms, clk
}

var ctx = context.Background()

func isInvalid(err error, substr string) bool {
	var ve domain.ValidationError
	return errors.As(err, &ve) && strings.Contains(string(ve), substr)
}

func TestRequestCodeValidation(t *testing.T) {
	s, _, _, _ := setup("")
	if _, err := s.RequestCode(ctx, "123"); !isInvalid(err, "формате") {
		t.Fatalf("плохой номер: %v", err)
	}
	if _, err := s.RequestCode(ctx, "+7 777 999 88 77"); !isInvalid(err, "нет в компании") {
		t.Fatalf("неизвестный номер: %v", err)
	}
}

func TestLoginHappyPathAndReuse(t *testing.T) {
	s, _, sms, _ := setup("")
	r, err := s.RequestCode(ctx, "8 701 000 00 01")
	if err != nil || r.Phone != "77010000001" {
		t.Fatalf("RequestCode: %+v %v", r, err)
	}
	token, u, err := s.VerifyCode(ctx, r.Phone, sms.code())
	if err != nil || token == "" || u.ID != 1 {
		t.Fatalf("VerifyCode: %v", err)
	}
	if got, err := s.SessionUser(ctx, token); err != nil || got.ID != 1 {
		t.Fatalf("SessionUser: %v", err)
	}
	if _, _, err := s.VerifyCode(ctx, r.Phone, sms.code()); !isInvalid(err, "истёк") {
		t.Fatalf("код сработал повторно: %v", err)
	}
	if err := s.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(ctx, token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("сессия жива после выхода")
	}
}

func TestWrongCodeAttempts(t *testing.T) {
	s, _, sms, _ := setup("")
	s.RequestCode(ctx, "77010000001")
	good := sms.code()
	wrong := "0000"
	if good == wrong {
		wrong = "1111"
	}
	_, _, err := s.VerifyCode(ctx, "77010000001", wrong)
	if !isInvalid(err, "Осталось попыток: 4") {
		t.Fatalf("первая ошибка: %v", err)
	}
	for i := 0; i < MaxAttempts-1; i++ {
		s.VerifyCode(ctx, "77010000001", wrong)
	}
	if _, _, err := s.VerifyCode(ctx, "77010000001", good); !isInvalid(err, "попытки закончились") {
		t.Fatalf("после %d ошибок верный код всё ещё работает: %v", MaxAttempts, err)
	}
}

func TestCodeExpiresAndResendThrottle(t *testing.T) {
	s, repo, sms, clk := setup("")
	s.RequestCode(ctx, "77010000001")
	code := sms.code()

	clk.t = clk.t.Add(10 * time.Second)
	r, _ := s.RequestCode(ctx, "77010000001")
	if r.Wait != 50 || len(repo.codes) != 1 {
		t.Fatalf("повторная отправка через 10 с: wait=%d, кодов=%d", r.Wait, len(repo.codes))
	}

	clk.t = clk.t.Add(CodeTTL)
	if _, _, err := s.VerifyCode(ctx, "77010000001", code); !isInvalid(err, "истёк") {
		t.Fatalf("истёкший код принят: %v", err)
	}
}

func TestFixedCode(t *testing.T) {
	s, _, _, _ := setup("1111")
	for i := 0; i < 2; i++ { // без ограничения на повторную отправку
		r, err := s.RequestCode(ctx, "77010000001")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.VerifyCode(ctx, r.Phone, "1111"); err != nil {
			t.Fatalf("вход %d с 1111: %v", i+1, err)
		}
	}
}

func TestCodesPerHourLimit(t *testing.T) {
	s, _, _, clk := setup("")
	for i := 0; i < MaxCodesPerHour; i++ {
		if _, err := s.RequestCode(ctx, "77010000001"); err != nil {
			t.Fatalf("код %d: %v", i+1, err)
		}
		clk.t = clk.t.Add(ResendAfter)
	}
	if _, err := s.RequestCode(ctx, "77010000001"); !isInvalid(err, "Слишком много") {
		t.Fatalf("шестой код за час: %v", err)
	}
	clk.t = clk.t.Add(time.Hour)
	if _, err := s.RequestCode(ctx, "77010000001"); err != nil {
		t.Fatalf("через час снова можно: %v", err)
	}
}
