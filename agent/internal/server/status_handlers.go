package server

import (
	"net/http"
	"runtime"
	"time"
)

type statusResponse struct {
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
	Platform      string `json:"platform"`
	FreeBytes     uint64 `json:"freeBytes"`
	TotalBytes    uint64 `json:"totalBytes"`
}

func statusHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		free, total, err := diskStats(cfg.DataDir)
		if err != nil {
			writeInternal(w, "status", err)
			return
		}
		writeJSON(w, http.StatusOK, statusResponse{
			Version:       cfg.Version,
			UptimeSeconds: int64(time.Since(cfg.StartTime).Seconds()),
			Platform:      runtime.GOOS + "/" + runtime.GOARCH,
			FreeBytes:     free,
			TotalBytes:    total,
		})
	}
}
