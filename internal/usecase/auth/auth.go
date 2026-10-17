// Package auth — вход по одноразовому коду из SMS и сессии.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"qauipsiz/internal/domain"
)

const (
	CodeTTL     = 5 * time.Minute
	ResendAfter = 60 * time.Second
	MaxAttempts = 5
	// MaxCodesPerHour вместе с MaxAttempts даёт не больше 25 попыток угадать код в час на номер.
	MaxCodesPerHour = 5
	SessionTTL      = 30 * 24 * time.Hour
)

type Code struct {
	ID       int64
	Hash     string
	Attempts int
}

type Repo interface {
	ActiveUserExists(ctx context.Context, phone string) (bool, error)
	LastCodeAt(ctx context.Context, phone string) (*time.Time, error)
	CodesSince(ctx context.Context, phone string, since time.Time) (int, error)
	SaveCode(ctx context.Context, phone, hash string, expires time.Time) error
	// ActiveCode — последний неиспользованный и неистёкший код; domain.ErrNotFound, если его нет.
	ActiveCode(ctx context.Context, phone string, now time.Time) (Code, error)
	IncAttempts(ctx context.Context, codeID int64) error
	// UseCode помечает код использованным; false — если его уже использовали.
	UseCode(ctx context.Context, codeID int64) (bool, error)
	CreateSession(ctx context.Context, phone, tokenHash string, expires time.Time) (domain.User, error)
	SessionUser(ctx context.Context, tokenHash string, now time.Time) (domain.User, error)
	DeleteSession(ctx context.Context, tokenHash string) error
}

type SMSSender interface {
	Send(phone, text string) error
}

type Service struct {
	repo Repo
	sms  SMSSender
	now  func() time.Time
	// fixedCode — временный общий код, пока не подключён SMS-провайдер. "" — случайный код.
	fixedCode string
}

func New(repo Repo, sms SMSSender, fixedCode string) *Service {
	return &Service{repo: repo, sms: sms, now: time.Now, fixedCode: fixedCode}
}

// WithClock подменяет часы (для тестов).
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

func (s *Service) FixedCode() string { return s.fixedCode }

// CodeRequest — результат запроса кода: нормализованный номер и сколько секунд ждать до повторной отправки.
type CodeRequest struct {
	Phone string
	Wait  int
}

func hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func (s *Service) RequestCode(ctx context.Context, rawPhone string) (CodeRequest, error) {
	phone := domain.NormalizePhone(rawPhone)
	if phone == "" {
		return CodeRequest{}, domain.Invalid("Введите номер в формате +7 7XX XXX XX XX")
	}
	ok, err := s.repo.ActiveUserExists(ctx, phone)
	if err != nil {
		return CodeRequest{}, err
	}
	if !ok {
		return CodeRequest{}, domain.Invalid("Этого номера нет в компании. Обратитесь к инженеру по ОТ.")
	}
	now := s.now()
	if s.fixedCode == "" {
		last, err := s.repo.LastCodeAt(ctx, phone)
		if err != nil {
			return CodeRequest{}, err
		}
		if last != nil && now.Sub(*last) < ResendAfter {
			return CodeRequest{Phone: phone, Wait: int((ResendAfter - now.Sub(*last)).Seconds())}, nil
		}
		n, err := s.repo.CodesSince(ctx, phone, now.Add(-time.Hour))
		if err != nil {
			return CodeRequest{}, err
		}
		if n >= MaxCodesPerHour {
			return CodeRequest{}, domain.Invalid("Слишком много попыток входа. Попробуйте через час.")
		}
	}
	code := s.fixedCode
	if code == "" {
		n, err := rand.Int(rand.Reader, big.NewInt(10000))
		if err != nil {
			return CodeRequest{}, err
		}
		code = fmt.Sprintf("%04d", n.Int64())
	}
	if err := s.repo.SaveCode(ctx, phone, hash(phone+code), now.Add(CodeTTL)); err != nil {
		return CodeRequest{}, err
	}
	if err := s.sms.Send(phone, "Qauipsiz: код входа "+code); err != nil {
		return CodeRequest{}, err
	}
	return CodeRequest{Phone: phone, Wait: int(ResendAfter.Seconds())}, nil
}

// VerifyCode проверяет код и открывает сессию. Возвращает токен для cookie.
func (s *Service) VerifyCode(ctx context.Context, rawPhone, code string) (string, domain.User, error) {
	phone := domain.NormalizePhone(rawPhone)
	c, err := s.repo.ActiveCode(ctx, phone, s.now())
	if errors.Is(err, domain.ErrNotFound) || (err == nil && c.Attempts >= MaxAttempts) {
		return "", domain.User{}, domain.Invalid("Код истёк или попытки закончились. Запросите новый.")
	}
	if err != nil {
		return "", domain.User{}, err
	}
	if hash(phone+code) != c.Hash {
		if err := s.repo.IncAttempts(ctx, c.ID); err != nil {
			return "", domain.User{}, err
		}
		return "", domain.User{}, domain.Invalid("Неверный код. Осталось попыток: " + strconv.Itoa(MaxAttempts-c.Attempts-1))
	}
	used, err := s.repo.UseCode(ctx, c.ID)
	if err != nil {
		return "", domain.User{}, err
	}
	if !used {
		return "", domain.User{}, domain.Invalid("Код уже использован")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", domain.User{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	u, err := s.repo.CreateSession(ctx, phone, hash(token), s.now().Add(SessionTTL))
	return token, u, err
}

func (s *Service) SessionUser(ctx context.Context, token string) (domain.User, error) {
	if token == "" {
		return domain.User{}, domain.ErrNotFound
	}
	return s.repo.SessionUser(ctx, hash(token), s.now())
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.repo.DeleteSession(ctx, hash(token))
}
