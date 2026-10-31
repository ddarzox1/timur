// Package auth implements login by IIN and password-backed sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math/big"
	"time"

	"golang.org/x/crypto/bcrypt"

	"qauipsiz/internal/domain"
)

const SessionTTL = 30 * 24 * time.Hour

type Credential struct {
	User         domain.User
	PasswordHash string
}

type Repo interface {
	LoginCredential(ctx context.Context, iin string) (Credential, error)
	CreateSession(ctx context.Context, userID int64, tokenHash string, expires time.Time) (domain.User, error)
	SessionUser(ctx context.Context, tokenHash string, now time.Time) (domain.User, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	PasswordHash(ctx context.Context, userID int64) (string, error)
	UpdatePassword(ctx context.Context, userID int64, passwordHash string) error
	TouchLastSeen(ctx context.Context, userID int64, before time.Time) error
}

type Service struct {
	repo Repo
	now  func() time.Time
}

func New(repo Repo) *Service { return &Service{repo: repo, now: time.Now} }

// WithClock replaces the clock in tests.
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

func (s *Service) TouchLastSeen(ctx context.Context, u domain.User) error {
	now := s.now()
	if u.LastSeenAt != nil && now.Sub(*u.LastSeenAt) < 5*time.Minute {
		return nil
	}
	return s.repo.TouchLastSeen(ctx, u.ID, now.Add(-5*time.Minute))
}

func hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)

// Login verifies an active user by the globally unique IIN and creates a session.
// The public error is deliberately identical for an unknown IIN and a bad password.
func (s *Service) Login(ctx context.Context, rawIIN, password string) (string, domain.User, error) {
	iin := domain.NormalizeIIN(rawIIN)
	if !domain.ValidIIN(iin) || password == "" {
		return "", domain.User{}, domain.Invalid("Неверный ИИН или пароль")
	}
	c, err := s.repo.LoginCredential(ctx, iin)
	if errors.Is(err, domain.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return "", domain.User{}, domain.Invalid("Неверный ИИН или пароль")
	}
	if err != nil {
		return "", domain.User{}, err
	}
	if c.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(c.PasswordHash), []byte(password)) != nil {
		return "", domain.User{}, domain.Invalid("Неверный ИИН или пароль")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", domain.User{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	u, err := s.repo.CreateSession(ctx, c.User.ID, hash(token), s.now().Add(SessionTTL))
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

func (s *Service) ChangePassword(ctx context.Context, u domain.User, current, next, confirm string) error {
	if next != confirm {
		return domain.Invalid("Новые пароли не совпадают")
	}
	if len(next) < 8 {
		return domain.Invalid("Новый пароль должен содержать не менее 8 символов")
	}
	if len(next) > 72 {
		return domain.Invalid("Пароль слишком длинный")
	}
	oldHash, err := s.repo.PasswordHash(ctx, u.ID)
	if err != nil {
		return err
	}
	if oldHash == "" || bcrypt.CompareHashAndPassword([]byte(oldHash), []byte(current)) != nil {
		return domain.Invalid("Текущий пароль указан неверно")
	}
	passwordHash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return s.repo.UpdatePassword(ctx, u.ID, passwordHash)
}

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

const passwordLetters = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
const passwordDigits = "23456789"

// NewInitialPassword returns exactly eight characters and always includes both letters and digits.
func NewInitialPassword() (string, error) {
	chars := make([]byte, 8)
	pick := func(alphabet string) (byte, error) {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return 0, err
		}
		return alphabet[n.Int64()], nil
	}
	var err error
	if chars[0], err = pick(passwordLetters); err != nil {
		return "", err
	}
	if chars[1], err = pick(passwordDigits); err != nil {
		return "", err
	}
	all := passwordLetters + passwordDigits
	for i := 2; i < len(chars); i++ {
		if chars[i], err = pick(all); err != nil {
			return "", err
		}
	}
	for i := len(chars) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		j := int(n.Int64())
		chars[i], chars[j] = chars[j], chars[i]
	}
	return string(chars), nil
}
