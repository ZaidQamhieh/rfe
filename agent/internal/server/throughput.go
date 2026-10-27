package server

import (
	"net/http"
	"sync/atomic"
	"time"
)

// rxBytesTotal/txBytesTotal are process-lifetime cumulative counters for
// bytes received (chunk uploads, whole-file content writes) and sent (file
// downloads) through the agent. The web companion polls /metrics and diffs
// successive readings client-side to draw a live bytes/sec chart — no
// server-side history is kept, so a reload just starts the chart over.
// cpuPercent/ramPercent (metrics_linux.go) are instantaneous point reads.
var rxBytesTotal atomic.Int64
var txBytesTotal atomic.Int64

type metricsResponse struct {
	RxBytes    int64   `json:"rxBytes"`
	TxBytes    int64   `json:"txBytes"`
	CPUPercent float64 `json:"cpuPercent"`
	RAMPercent float64 `json:"ramPercent"`
	TsMs       int64   `json:"tsMs"`
}

func metricsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, metricsResponse{
			RxBytes:    rxBytesTotal.Load(),
			TxBytes:    txBytesTotal.Load(),
			CPUPercent: cpuPercent(),
			RAMPercent: ramPercent(),
			TsMs:       time.Now().UnixMilli(),
		})
	}
}

// countingWriter tallies every byte written to an http.ResponseWriter into
// txBytesTotal — used to count download bytes at the one place they all
// funnel through (downloadHandler), regardless of whether the response is
// gzip'd or served via http.ServeContent.
type countingWriter struct {
	http.ResponseWriter
}

func (c countingWriter) Write(p []byte) (int, error) {
	n, err := c.ResponseWriter.Write(p)
	txBytesTotal.Add(int64(n))
	return n, err
}
