package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config 汇聚全部运行时配置，值来自环境变量（见 .env.example）。
// 不引入第三方配置库，保持依赖最小化。
type Config struct {
	Env     string
	LogLvl  string
	APIAddr string

	PostgresDSN string

	RedisAddr     string
	RedisPassword string

	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	S3UseSSL    bool

	JWTSecret string
	JWTTTL    time.Duration
}

// devJWTSecret 仅供本地开发使用。生产环境必须显式提供 JWT_SECRET，
// 否则启动即失败——避免默认密钥泄露后任何人可伪造令牌。
const devJWTSecret = "dev-only-change-me"

// minJWTSecretLen 生产环境要求的最小密钥长度（字节）。
const minJWTSecretLen = 32

// Load 从环境变量构建配置，缺失的变量回退到开发默认值。
func Load() Config {
	return Config{
		Env:     getenv("APP_ENV", "development"),
		LogLvl:  getenv("LOG_LEVEL", "info"),
		APIAddr: getenv("API_ADDR", ":8080"),

		PostgresDSN: getenv("POSTGRES_DSN",
			"postgres://thesis:thesis_dev@localhost:5432/thesis?sslmode=disable"),

		RedisAddr:     getenv("REDIS_HOST", "localhost") + ":" + getenv("REDIS_PORT", "6379"),
		RedisPassword: getenv("REDIS_PASSWORD", ""),

		S3Endpoint:  getenv("S3_ENDPOINT", "localhost:9000"),
		S3AccessKey: getenv("S3_ACCESS_KEY", "minioadmin"),
		S3SecretKey: getenv("S3_SECRET_KEY", "minioadmin"),
		S3Bucket:    getenv("S3_BUCKET", "thesis-files"),
		S3UseSSL:    getenv("S3_USE_SSL", "false") == "true",

		JWTSecret: getenv("JWT_SECRET", devJWTSecret),
		JWTTTL:    time.Hour * 168,
	}
}

// Validate 校验配置的可用性。生产环境不允许使用默认 JWT 密钥，
// 且要求密钥长度足够（防止暴力猜测）。
func (c Config) Validate() error {
	if c.IsProduction() {
		if c.JWTSecret == devJWTSecret {
			return fmt.Errorf("生产环境必须显式设置 JWT_SECRET，禁止使用默认值")
		}
		if len(c.JWTSecret) < minJWTSecretLen {
			return fmt.Errorf("生产环境 JWT_SECRET 长度至少 %d 字节，当前 %d 字节", minJWTSecretLen, len(c.JWTSecret))
		}
	}
	return nil
}

// IsProduction 判断当前是否运行在生产环境。
func (c Config) IsProduction() bool {
	env := strings.ToLower(strings.TrimSpace(c.Env))
	return env == "production" || env == "prod"
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
