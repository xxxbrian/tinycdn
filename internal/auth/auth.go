package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	defaultTokenTTL  = 24 * time.Hour
	defaultUsername  = "admin"
	defaultPassword  = "awesometinycdn"
	defaultJWTSecret = "awesometinycdn"
	tokenIssuer      = "tinycdn"
	tokenAudience    = "admin"
	tokenRole        = "owner"
)

var (
	ErrMissingConfig     = errors.New("admin auth configuration is incomplete")
	ErrInvalidCredential = errors.New("invalid username or password")
	ErrInvalidToken      = errors.New("invalid token")
)

type Config struct {
	Username     string
	Password     string
	PasswordHash string
	JWTSecret    []byte
	TokenTTL     time.Duration
}

type Identity struct {
	Subject   string    `json:"sub"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type tokenHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type tokenClaims struct {
	Sub      string `json:"sub"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Iss      string `json:"iss"`
	Aud      string `json:"aud"`
	Iat      int64  `json:"iat"`
	Exp      int64  `json:"exp"`
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		Username:     defaultString(strings.TrimSpace(os.Getenv("TINYCDN_ADMIN_USERNAME")), defaultUsername),
		Password:     defaultString(os.Getenv("TINYCDN_ADMIN_PASSWORD"), defaultPassword),
		PasswordHash: strings.TrimSpace(os.Getenv("TINYCDN_ADMIN_PASSWORD_HASH")),
		JWTSecret:    []byte(defaultString(os.Getenv("TINYCDN_ADMIN_JWT_SECRET"), defaultJWTSecret)),
		TokenTTL:     defaultTokenTTL,
	}
	if rawTTL := strings.TrimSpace(os.Getenv("TINYCDN_ADMIN_TOKEN_TTL")); rawTTL != "" {
		ttl, err := time.ParseDuration(rawTTL)
		if err != nil {
			return Config{}, fmt.Errorf("invalid TINYCDN_ADMIN_TOKEN_TTL: %w", err)
		}
		if ttl <= 0 {
			return Config{}, fmt.Errorf("invalid TINYCDN_ADMIN_TOKEN_TTL: must be positive")
		}
		cfg.TokenTTL = ttl
	}
	if cfg.Username == "" || len(cfg.JWTSecret) == 0 || (cfg.Password == "" && cfg.PasswordHash == "") {
		return Config{}, ErrMissingConfig
	}
	return cfg, nil
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (c Config) Authenticate(username, password string) (Identity, error) {
	if subtle.ConstantTimeCompare([]byte(username), []byte(c.Username)) != 1 {
		return Identity{}, ErrInvalidCredential
	}
	if c.PasswordHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(c.PasswordHash), []byte(password)); err != nil {
			return Identity{}, ErrInvalidCredential
		}
	} else if subtle.ConstantTimeCompare([]byte(password), []byte(c.Password)) != 1 {
		return Identity{}, ErrInvalidCredential
	}
	return Identity{
		Subject:  "admin",
		Username: c.Username,
		Role:     tokenRole,
	}, nil
}

func (c Config) Issue(identity Identity, now time.Time) (string, Identity, error) {
	if identity.Subject == "" {
		identity.Subject = "admin"
	}
	if identity.Username == "" {
		identity.Username = c.Username
	}
	if identity.Role == "" {
		identity.Role = tokenRole
	}
	identity.IssuedAt = now.UTC()
	identity.ExpiresAt = identity.IssuedAt.Add(c.TokenTTL)

	headerPayload, err := encodeTokenPart(tokenHeader{Alg: "HS256", Typ: "JWT"})
	if err != nil {
		return "", Identity{}, err
	}
	claimsPayload, err := encodeTokenPart(tokenClaims{
		Sub:      identity.Subject,
		Username: identity.Username,
		Role:     identity.Role,
		Iss:      tokenIssuer,
		Aud:      tokenAudience,
		Iat:      identity.IssuedAt.Unix(),
		Exp:      identity.ExpiresAt.Unix(),
	})
	if err != nil {
		return "", Identity{}, err
	}

	unsigned := headerPayload + "." + claimsPayload
	signature := sign(unsigned, c.JWTSecret)
	return unsigned + "." + signature, identity, nil
}

func (c Config) Verify(token string, now time.Time) (Identity, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Identity{}, ErrInvalidToken
	}
	if parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Identity{}, ErrInvalidToken
	}
	expected := sign(parts[0]+"."+parts[1], c.JWTSecret)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[2])) != 1 {
		return Identity{}, ErrInvalidToken
	}

	var header tokenHeader
	if err := decodeTokenPart(parts[0], &header); err != nil {
		return Identity{}, ErrInvalidToken
	}
	if header.Alg != "HS256" || header.Typ != "JWT" {
		return Identity{}, ErrInvalidToken
	}

	var claims tokenClaims
	if err := decodeTokenPart(parts[1], &claims); err != nil {
		return Identity{}, ErrInvalidToken
	}
	if claims.Iss != tokenIssuer || claims.Aud != tokenAudience {
		return Identity{}, ErrInvalidToken
	}
	issuedAt := time.Unix(claims.Iat, 0).UTC()
	expiresAt := time.Unix(claims.Exp, 0).UTC()
	if now.UTC().After(expiresAt) {
		return Identity{}, ErrInvalidToken
	}
	if issuedAt.After(now.UTC().Add(5 * time.Minute)) {
		return Identity{}, ErrInvalidToken
	}

	return Identity{
		Subject:   claims.Sub,
		Username:  claims.Username,
		Role:      claims.Role,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	}, nil
}

func encodeTokenPart(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeTokenPart[T any](raw string, out *T) error {
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, out)
}

func sign(unsigned string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(unsigned))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
