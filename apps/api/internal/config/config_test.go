package config

import "testing"

func TestValidateRejectsDevSecretInProduction(t *testing.T) {
	cfg := Config{Env: "production", JWTSecret: devJWTSecret}
	if err := cfg.Validate(); err == nil {
		t.Fatal("生产环境使用默认密钥应被拒绝")
	}
}

func TestValidateRejectsShortSecretInProduction(t *testing.T) {
	cfg := Config{Env: "prod", JWTSecret: "short"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("生产环境短密钥应被拒绝")
	}
}

func TestValidateAllowsStrongSecretInProduction(t *testing.T) {
	cfg := Config{Env: "production", JWTSecret: "0123456789abcdef0123456789abcdef"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("强密钥应通过: %v", err)
	}
}

func TestValidateSkipsChecksInDevelopment(t *testing.T) {
	cfg := Config{Env: "development", JWTSecret: devJWTSecret}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("开发环境不应校验密钥: %v", err)
	}
}
