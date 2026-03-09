package auth

import (
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestConfigAuthenticateAndVerify(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("generate hash: %v", err)
	}
	cfg := Config{
		Username:     "admin",
		PasswordHash: string(hash),
		JWTSecret:    []byte("super-secret"),
		TokenTTL:     2 * time.Hour,
	}

	identity, err := cfg.Authenticate("admin", "secret-password")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if _, err := cfg.Authenticate("admin", "wrong-password"); err == nil {
		t.Fatalf("expected wrong password to fail")
	}

	now := time.Date(2026, time.March, 9, 0, 0, 0, 0, time.UTC)
	token, issued, err := cfg.Issue(identity, now)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if issued.ExpiresAt.Sub(issued.IssuedAt) != cfg.TokenTTL {
		t.Fatalf("unexpected token ttl %s", issued.ExpiresAt.Sub(issued.IssuedAt))
	}

	verified, err := cfg.Verify(token, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if verified.Username != "admin" || verified.Role != tokenRole {
		t.Fatalf("unexpected identity %#v", verified)
	}
	if _, err := cfg.Verify(token, now.Add(3*time.Hour)); err == nil {
		t.Fatalf("expected expired token to fail")
	}
}

func TestLoadConfigFromEnvUsesDefaults(t *testing.T) {
	t.Setenv("TINYCDN_ADMIN_USERNAME", "")
	t.Setenv("TINYCDN_ADMIN_PASSWORD", "")
	t.Setenv("TINYCDN_ADMIN_PASSWORD_HASH", "")
	t.Setenv("TINYCDN_ADMIN_JWT_SECRET", "")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Username != defaultUsername {
		t.Fatalf("unexpected default username %q", cfg.Username)
	}
	if cfg.Password != defaultPassword {
		t.Fatalf("unexpected default password %q", cfg.Password)
	}
	if string(cfg.JWTSecret) != defaultJWTSecret {
		t.Fatalf("unexpected default jwt secret %q", string(cfg.JWTSecret))
	}
}

func TestLoadConfigFromEnvAllowsOverrides(t *testing.T) {
	t.Setenv("TINYCDN_ADMIN_USERNAME", "root")
	t.Setenv("TINYCDN_ADMIN_PASSWORD", "secret")
	t.Setenv("TINYCDN_ADMIN_PASSWORD_HASH", "")
	t.Setenv("TINYCDN_ADMIN_JWT_SECRET", "super-secret")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Username != "root" {
		t.Fatalf("unexpected username %q", cfg.Username)
	}
	if cfg.Password != "secret" {
		t.Fatalf("unexpected password %q", cfg.Password)
	}
	if string(cfg.JWTSecret) != "super-secret" {
		t.Fatalf("unexpected secret %q", string(cfg.JWTSecret))
	}
}
