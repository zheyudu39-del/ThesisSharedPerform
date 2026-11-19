package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Health 健康检查响应。
type Health struct {
	Status   string `json:"status"`
	Env      string `json:"env"`
	Database string `json:"database"`
	Version  string `json:"version"`
}

// HealthCheck 返回服务存活、数据库连通性与数据库版本。
func HealthCheck(env, version string, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		dbStatus := "down"
		if err := pool.Ping(ctx); err == nil {
			dbStatus = "up"
		}

		code := http.StatusOK
		status := "ok"
		if dbStatus != "up" {
			code = http.StatusServiceUnavailable
			status = "degraded"
		}

		writeJSON(w, code, Health{
			Status:   status,
			Env:      env,
			Database: dbStatus,
			Version:  version,
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
