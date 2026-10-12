package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/httpx"
	"golang.org/x/crypto/bcrypt"
)

const SessionLifetime = 12 * time.Hour

type Service struct {
	Repo      *Repository
	dummyHash []byte
}

func NewService(repo *Repository) (*Service, error) {
	token, err := Token()
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &Service{Repo: repo, dummyHash: hash}, nil
}
func Token() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
func Hash(token string) []byte { hash := sha256.Sum256([]byte(token)); return hash[:] }
func ValidToken(token string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) == 32 && len(token) == 43
}

type LoginResult struct {
	User        users.User
	Token, CSRF string
	Expires     time.Time
}

func (s *Service) Login(ctx context.Context, email, password, ip string, previousToken ...string) (LoginResult, error) {
	var result LoginResult
	allowed, err := s.Repo.AllowIP(ctx, Hash(ip))
	if err != nil {
		return result, err
	}
	if !allowed {
		return result, httpx.E(429, "rate_limited", "Слишком много попыток входа. Повторите через 15 минут.")
	}
	normalized, validation := users.NormalizeEmail(email)
	if validation != nil {
		normalized = ""
	}
	result.Token, err = Token()
	if err != nil {
		return result, err
	}
	result.CSRF, err = Token()
	if err != nil {
		return result, err
	}
	result.Expires = time.Now().UTC().Add(SessionLifetime)
	var authenticated bool
	var previousHash []byte
	if len(previousToken) > 0 && ValidToken(previousToken[0]) {
		previousHash = Hash(previousToken[0])
	}
	err = s.Repo.LoginTransaction(ctx, normalized, func(a Account, tx *LoginTx) error {
		hash := []byte(a.Hash)
		if a.ID == 0 {
			hash = s.dummyHash
		}
		matches := bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
		now := time.Now().UTC()
		if a.ID == 0 || a.Blocked || (a.LockedUntil != nil && now.Before(*a.LockedUntil)) || !matches {
			// Attempts during an existing lock must not extend it indefinitely.
			if a.LockedUntil != nil && now.Before(*a.LockedUntil) {
				return tx.LockedFailure(ctx, a.ID)
			}
			return tx.Failure(ctx, a, now)
		}
		var err error
		result.User, err = tx.Success(ctx, a, Hash(result.Token), Hash(result.CSRF), previousHash, result.Expires)
		authenticated = err == nil
		return err
	})
	if err != nil {
		return LoginResult{}, err
	}
	if !authenticated {
		return LoginResult{}, httpx.E(401, "invalid_credentials", "Неверный email или пароль.")
	}
	return result, nil
}

func (s *Service) Logout(ctx context.Context, token string, id int64) error {
	return s.Repo.Logout(ctx, Hash(token), id)
}

func (s *Service) RotateCSRF(ctx context.Context, sessionToken, csrf string) error {
	return s.Repo.RotateCSRF(ctx, Hash(sessionToken), Hash(csrf))
}
func (s *Service) Current(ctx context.Context, token string) (Session, error) {
	if !ValidToken(token) {
		return Session{}, httpx.Unauthorized()
	}
	return s.Repo.Current(ctx, Hash(token))
}
func (s *Service) ChangePassword(ctx context.Context, id int64, token, oldPassword, newPassword string) error {
	hash, err := users.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if oldPassword == newPassword {
		return httpx.Invalid("password", "Новый пароль должен отличаться от текущего.")
	}
	return s.Repo.ChangePassword(ctx, id, Hash(token), hash, func(oldHash string) bool {
		return bcrypt.CompareHashAndPassword([]byte(oldHash), []byte(oldPassword)) == nil
	})
}
