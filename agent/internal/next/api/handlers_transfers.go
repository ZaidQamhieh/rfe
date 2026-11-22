package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// Header, query and field names of the transfer routes.
const (
	headerChunkSHA256 = "X-Chunk-Sha256"
	queryDevice       = "device"
	queryUser         = "user"
	pathChunkIndex    = "n"
	fieldSHA256       = "sha256"
	fieldVerified     = "verified"
)

// Limits of the transfer routes.
const (
	// DefaultChunkBufferBytes is how much chunk body data all uploads together
	// may hold in memory at once (the old agent's 256 MiB).
	DefaultChunkBufferBytes int64 = 256 << 20
	// DefaultChunkWait is how long a chunk waits for room before the answer is
	// 429 CHUNK_BUSY.
	DefaultChunkWait = 5 * time.Second
	// MaxTransferListRows caps the rows of GET /transfers/list.
	MaxTransferListRows = 200
	// chunkRetryAfter is the Retry-After value of CHUNK_BUSY, in seconds.
	chunkRetryAfter = 2
	// activeTransferWindow is how recent a chunk must be for a session to count
	// as active now.
	activeTransferWindow = 30 * time.Second
	// percentScale turns a received/total ratio into the 0-100 progress.
	percentScale = 100
)

// Client-safe texts of the transfer routes. None carries request data.
const (
	msgTransferNotFound = "transfer not found"
	msgChunkIndex       = "chunk index must be a whole number inside the session"
	msgChunkHashHeader  = headerChunkSHA256 + " header is required"
	msgChunkTooLarge    = "chunk is larger than the session allows"
	msgChunkRead        = "the chunk body could not be read"
	msgDeviceRequired   = "a device is required"
)

// TransferOpen is a request to start an upload session. The ID is chosen by the
// handler, so a retried open with the same ID is the service's concern.
type TransferOpen struct {
	ID        string
	Path      string
	Size      int64
	ChunkSize int
	SHA256    string
	Overwrite bool
}

// Transfers is the upload engine the transfer routes use. It is an interface of
// its own because the engine imports this package for its error kinds, so this
// package cannot import the engine; the composition root adapts the engine to
// it and binds the caller's view of the disk (jail, read-only) from p and pol.
// Every method answers ErrNotFound for a session the caller may not use.
type Transfers interface {
	// Open starts a session, or returns the caller's open one with the same ID.
	Open(p authz.Principal, pol authz.Policy, req TransferOpen) (*store.Upload, error)
	// Lookup returns the session row without chunk numbers or touching it.
	Lookup(p authz.Principal, id string) (*store.Upload, error)
	// Status returns the session with its received chunk numbers and marks it active.
	Status(p authz.Principal, id string) (*store.Upload, error)
	// Delete removes an inactive session and its temporary file.
	Delete(p authz.Principal, id string) error
	// WriteChunk stores chunk n after checking its length and SHA-256.
	WriteChunk(p authz.Principal, id string, n int, data []byte, chunkSHA256 string) error
	// Complete verifies the whole file and publishes it. The entry marshals to
	// the contract Entry object.
	Complete(p authz.Principal, pol authz.Policy, id string) (entry any, err error)
}

// UploadLister reads every upload session row for the list route. *store.DB
// satisfies it.
type UploadLister interface {
	Uploads() ([]store.Upload, error)
}

// UploadSessionJSON is an upload session (openapi UploadSession).
type UploadSessionJSON struct {
	ID             string `json:"id"`
	Path           string `json:"path"`
	Size           int64  `json:"size"`
	ChunkSize      int    `json:"chunkSize"`
	TotalChunks    int    `json:"totalChunks"`
	ReceivedChunks []int  `json:"receivedChunks"`
	Status         string `json:"status"`
}

// TransferSummaryJSON is one row of the transfer list (openapi TransferSummary).
type TransferSummaryJSON struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Path          string  `json:"path"`
	TotalSize     int64   `json:"totalSize"`
	ReceivedBytes int64   `json:"receivedBytes"`
	Progress      float64 `json:"progress"`
	Status        string  `json:"status"`
	DeviceID      string  `json:"deviceId"`
	UpdatedAt     int64   `json:"updatedAt"`
}

// TransferDeviceJSON is a device with at least one transfer, for the filter row.
type TransferDeviceJSON struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Username string `json:"username"`
}

// TransferListJSON is the answer of GET /transfers/list.
type TransferListJSON struct {
	Total     int                   `json:"total"`
	Counts    map[string]int        `json:"counts"`
	ActiveNow int                   `json:"activeNow"`
	Devices   []TransferDeviceJSON  `json:"devices"`
	Users     []string              `json:"users"`
	Transfers []TransferSummaryJSON `json:"transfers"`
}

// openTransferBody is the JSON body of POST /transfers.
type openTransferBody struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	ChunkSize int    `json:"chunkSize"`
	Overwrite bool   `json:"overwrite"`
}

// chunkRoutePattern is the full pattern of the chunk upload route. Its body is
// file data, so the middleware leaves the global body limit off it and the
// handler bounds the body by the session's chunk size instead.
var chunkRoutePattern = put + " " + PathPrefix + "/transfers/{id}/chunks/{" + pathChunkIndex + "}"

// ownsBodyLimit reports whether the route behind pattern bounds its own body.
func ownsBodyLimit(pattern string) bool { return pattern == chunkRoutePattern }

// byteBudget hands out a fixed number of bytes of chunk buffer; a request that
// cannot get its bytes in time is told to retry.
type byteBudget struct {
	mu    sync.Mutex
	total int64
	free  int64
	wake  chan struct{}
}

// newByteBudget makes a budget of total bytes.
func newByteBudget(total int64) *byteBudget {
	return &byteBudget{total: total, free: total, wake: make(chan struct{})}
}

// acquire reserves n bytes (at most the whole budget, so one chunk can always
// run alone), waiting up to wait or until done closes. It reports whether it
// got them.
func (b *byteBudget) acquire(n int64, wait time.Duration, done <-chan struct{}) (int64, bool) {
	if n > b.total {
		n = b.total
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		b.mu.Lock()
		if b.free >= n {
			b.free -= n
			b.mu.Unlock()
			return n, true
		}
		wake := b.wake
		b.mu.Unlock()
		select {
		case <-wake:
		case <-timer.C:
			return 0, false
		case <-done:
			return 0, false
		}
	}
}

// release returns n bytes to the budget and wakes the waiting requests.
func (b *byteBudget) release(n int64) {
	b.mu.Lock()
	b.free += n
	close(b.wake)
	b.wake = make(chan struct{})
	b.mu.Unlock()
}

// transferHandlers serves the transfer routes of one Server.
type transferHandlers struct {
	s      *Server
	budget *byteBudget
	wait   time.Duration
}

// registerTransferHandlers adds the transfer routes to the handler registry m.
// The chunk memory budget belongs to the Server it is built for.
func registerTransferHandlers(s *Server, m map[string]handlerFunc) {
	total, wait := s.cfg.ChunkBufferBytes, s.cfg.ChunkWait
	if total <= 0 {
		total = DefaultChunkBufferBytes
	}
	if wait <= 0 {
		wait = DefaultChunkWait
	}
	h := &transferHandlers{s: s, budget: newByteBudget(total), wait: wait}
	m[routeKey(post, "/transfers")] = h.open
	m[routeKey(get, "/transfers/list")] = h.list
	m[routeKey(get, "/transfers/{id}")] = h.status
	m[routeKey(del, "/transfers/{id}")] = h.remove
	m[routeKey(put, "/transfers/{id}/chunks/{"+pathChunkIndex+"}")] = h.chunk
	m[routeKey(post, "/transfers/{id}/complete")] = h.complete
}

// engine returns the transfer service, or the not-implemented answer when the
// server was built without one.
func (h *transferHandlers) engine() (Transfers, error) {
	if h.s.cfg.Transfers == nil {
		return nil, notYet
	}
	return h.s.cfg.Transfers, nil
}

// sessionJSON converts a stored session; a row without chunks reports an empty
// list, never null.
func sessionJSON(u *store.Upload) UploadSessionJSON {
	chunks := u.Chunks
	if chunks == nil {
		chunks = []int{}
	}
	return UploadSessionJSON{
		ID: u.ID, Path: u.TargetPath, Size: u.TotalSize, ChunkSize: u.ChunkSize,
		TotalChunks: u.TotalChunks, ReceivedChunks: chunks, Status: u.Status,
	}
}

// notFoundTransfer is the answer for a missing session.
func notFoundTransfer() error { return &apiError{kind: ErrNotFound, msg: msgTransferNotFound} }

// open serves POST /transfers: it checks the path and overwrite rights, then
// asks the engine for a new session under a fresh id.
func (h *transferHandlers) open(w http.ResponseWriter, r *http.Request) error {
	eng, err := h.engine()
	if err != nil {
		return err
	}
	c, err := callOf(r)
	if err != nil {
		return err
	}
	var body openTransferBody
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if body.Path == "" {
		return badRequest(msgPathRequired)
	}
	action := authz.ActionUpload
	if body.Overwrite {
		action = authz.ActionUploadOverwrite
	}
	if d := authz.Decide(c.principal, action, authz.Target{Paths: []string{body.Path}}, c.policy); !d.Allowed {
		return d.Err
	}
	u, err := eng.Open(c.principal, c.policy, TransferOpen{
		ID: uuid.NewString(), Path: body.Path, Size: body.Size, ChunkSize: body.ChunkSize,
		SHA256: body.SHA256, Overwrite: body.Overwrite,
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, sessionJSON(u))
	return nil
}

// status serves GET /transfers/{id}: the session and the chunks it already has.
func (h *transferHandlers) status(w http.ResponseWriter, r *http.Request) error {
	eng, err := h.engine()
	if err != nil {
		return err
	}
	c, err := callOf(r)
	if err != nil {
		return err
	}
	u, err := eng.Status(c.principal, r.PathValue("id"))
	if err != nil {
		return err
	}
	if u == nil {
		return notFoundTransfer()
	}
	writeJSON(w, http.StatusOK, sessionJSON(u))
	return nil
}

// remove serves DELETE /transfers/{id}.
func (h *transferHandlers) remove(w http.ResponseWriter, r *http.Request) error {
	eng, err := h.engine()
	if err != nil {
		return err
	}
	c, err := callOf(r)
	if err != nil {
		return err
	}
	if err := eng.Delete(c.principal, r.PathValue("id")); err != nil {
		return err
	}
	writeNoContent(w)
	return nil
}

// expectedChunkLen is the byte length chunk n of session u must have: a full
// chunk, or what is left of the file for the last one.
func expectedChunkLen(u *store.Upload, n int) int {
	left := u.TotalSize - int64(n)*int64(u.ChunkSize)
	switch {
	case left <= 0:
		return 0
	case left < int64(u.ChunkSize):
		return int(left)
	}
	return u.ChunkSize
}

// readChunk reads exactly want bytes from body, plus one probe byte to detect a
// longer body. A short body is returned as it is (the engine rejects its
// length); an over-long one is PAYLOAD_TOO_LARGE.
func readChunk(body io.Reader, want int) ([]byte, error) {
	buf := make([]byte, want)
	got, err := io.ReadFull(body, buf)
	switch {
	case err == nil:
		var probe [1]byte
		if _, perr := io.ReadFull(body, probe[:]); perr == nil {
			return nil, &apiError{kind: ErrPayloadTooLarge, msg: msgChunkTooLarge}
		} else if !errors.Is(perr, io.EOF) {
			err = perr
		}
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return buf[:got], nil
	}
	if err == nil {
		return buf, nil
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return nil, &apiError{kind: ErrPayloadTooLarge, msg: msgChunkTooLarge}
	}
	return nil, badRequest(msgChunkRead)
}

// chunk serves PUT /transfers/{id}/chunks/{n}. The body is bounded by the
// session's chunk size (the global limit does not apply to this route), read
// into one buffer of the chunk's exact length that is counted against the
// shared memory budget, then handed to the engine, which checks its SHA-256 and
// stores it. Sending a chunk the engine already has is a success.
func (h *transferHandlers) chunk(w http.ResponseWriter, r *http.Request) error {
	eng, err := h.engine()
	if err != nil {
		return err
	}
	c, err := callOf(r)
	if err != nil {
		return err
	}
	id := r.PathValue("id")
	n, err := strconv.Atoi(r.PathValue(pathChunkIndex))
	if err != nil || n < 0 {
		return badRequest(msgChunkIndex)
	}
	sum := r.Header.Get(headerChunkSHA256)
	if sum == "" {
		return badRequest(msgChunkHashHeader)
	}
	u, err := eng.Lookup(c.principal, id)
	if err != nil {
		return err
	}
	if u == nil {
		return notFoundTransfer()
	}
	if err := authorizeOverwrite(c, u); err != nil {
		return err
	}
	if n >= u.TotalChunks {
		return badRequest(msgChunkIndex)
	}
	want := expectedChunkLen(u, n)
	body := http.MaxBytesReader(w, r.Body, int64(u.ChunkSize))
	held, ok := h.budget.acquire(int64(want), h.wait, r.Context().Done())
	if !ok {
		w.Header().Set(headerRetryAfter, strconv.Itoa(chunkRetryAfter))
		return ErrChunkBusy
	}
	defer h.budget.release(held)
	data, err := readChunk(paceReaderShared(body, h.s.uploadRate(), r.Context(), &h.s.upPacer), want)
	if err != nil {
		return err
	}
	if err := eng.WriteChunk(c.principal, id, n, data, sum); err != nil {
		return err
	}
	writeNoContent(w)
	return nil
}

// complete serves POST /transfers/{id}/complete. The engine verifies and
// publishes the file; the answer is the file's Entry plus the verified hash.
func (h *transferHandlers) complete(w http.ResponseWriter, r *http.Request) error {
	eng, err := h.engine()
	if err != nil {
		return err
	}
	c, err := callOf(r)
	if err != nil {
		return err
	}
	id := r.PathValue("id")
	u, err := eng.Lookup(c.principal, id)
	if err != nil {
		return err
	}
	if u == nil {
		return notFoundTransfer()
	}
	if err := authorizeOverwrite(c, u); err != nil {
		return err
	}
	entry, err := eng.Complete(c.principal, c.policy, id)
	if err != nil {
		return err
	}
	// Merge the Entry's own JSON with the two verification fields; raw values
	// keep large numbers exact.
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if fields[fieldSHA256], err = json.Marshal(u.SHA256); err != nil {
		return err
	}
	fields[fieldVerified] = json.RawMessage("true")
	writeJSON(w, http.StatusOK, fields)
	return nil
}

// list serves GET /transfers/list. An admin sees every session, the whole-table
// counts and the device and user filters; any other device sees only its own
// sessions and counts derived from them, and its filter parameters are ignored.
func (h *transferHandlers) list(w http.ResponseWriter, r *http.Request) error {
	if h.s.cfg.Uploads == nil {
		return notYet
	}
	c, err := callOf(r)
	if err != nil {
		return err
	}
	if c.principal.DeviceID == "" {
		return &apiError{kind: authz.ErrForbidden, msg: msgDeviceRequired}
	}
	all, err := h.s.cfg.Uploads.Uploads()
	if err != nil {
		return err
	}
	now := h.s.clock.Now()
	out := TransferListJSON{Counts: map[string]int{}, Devices: []TransferDeviceJSON{}, Users: []string{}, Transfers: []TransferSummaryJSON{}}
	var rows []store.Upload
	if c.principal.Admin {
		devices, err := h.s.cfg.Devices.Devices()
		if err != nil {
			return err
		}
		rows = filterUploads(all, r.URL.Query().Get(queryDevice), r.URL.Query().Get(queryUser), devices)
		out.Devices, out.Users = transferFilters(all, devices)
		for _, u := range all {
			out.Counts[u.Status]++
		}
		out.ActiveNow = countActive(all, now)
	} else {
		for _, u := range all {
			if u.DeviceID == c.principal.DeviceID {
				rows = append(rows, u)
			}
		}
		sortRecent(rows)
		rows = capRows(rows)
		for _, u := range rows {
			out.Counts[u.Status]++
		}
		out.ActiveNow = countActive(rows, now)
	}
	for _, n := range out.Counts {
		out.Total += n
	}
	for _, u := range rows {
		out.Transfers = append(out.Transfers, summaryJSON(u))
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// sortRecent orders sessions newest activity first, ties by id.
func sortRecent(rows []store.Upload) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].UpdatedAt != rows[j].UpdatedAt {
			return rows[i].UpdatedAt > rows[j].UpdatedAt
		}
		return rows[i].ID < rows[j].ID
	})
}

// capRows keeps the most recent MaxTransferListRows of rows (already sorted).
func capRows(rows []store.Upload) []store.Upload {
	if len(rows) > MaxTransferListRows {
		return rows[:MaxTransferListRows]
	}
	return rows
}

// filterUploads applies an admin's device and user filters (both must match),
// then returns the newest rows up to the list cap.
func filterUploads(all []store.Upload, device, user string, devices []store.Device) []store.Upload {
	var byUser map[string]bool
	if user != "" {
		byUser = map[string]bool{}
		for _, d := range devices {
			if d.Username == user {
				byUser[d.ID] = true
			}
		}
	}
	var rows []store.Upload
	for _, u := range all {
		if device != "" && u.DeviceID != device {
			continue
		}
		if user != "" && !byUser[u.DeviceID] {
			continue
		}
		rows = append(rows, u)
	}
	sortRecent(rows)
	return capRows(rows)
}

// transferFilters lists the devices that opened a session, and the distinct
// login accounts behind them, both in a stable order.
func transferFilters(all []store.Upload, devices []store.Device) ([]TransferDeviceJSON, []string) {
	known := make(map[string]store.Device, len(devices))
	for _, d := range devices {
		known[d.ID] = d
	}
	seen := map[string]bool{}
	outDevices := []TransferDeviceJSON{}
	accounts := map[string]bool{}
	for _, u := range all {
		if u.DeviceID == "" || seen[u.DeviceID] {
			continue
		}
		seen[u.DeviceID] = true
		d := known[u.DeviceID]
		outDevices = append(outDevices, TransferDeviceJSON{ID: u.DeviceID, Label: d.Label, Username: d.Username})
		if d.Username != "" {
			accounts[d.Username] = true
		}
	}
	sort.Slice(outDevices, func(i, j int) bool { return outDevices[i].ID < outDevices[j].ID })
	users := make([]string, 0, len(accounts))
	for name := range accounts {
		users = append(users, name)
	}
	sort.Strings(users)
	return outDevices, users
}

// countActive counts open sessions that received a chunk inside the active
// window, as the old agent did. A session opened a moment ago but still empty
// is not active: its updated_at is the opening time, so the chunk list decides.
func countActive(rows []store.Upload, now time.Time) int {
	cutoff := now.Add(-activeTransferWindow).Unix()
	n := 0
	for _, u := range rows {
		if u.Status == store.UploadOpen && len(u.Chunks) > 0 && u.UpdatedAt >= cutoff {
			n++
		}
	}
	return n
}

// summaryJSON converts a row; received bytes are an estimate (full chunks times
// the chunk size, capped at the file size) and progress is by chunk count.
func summaryJSON(u store.Upload) TransferSummaryJSON {
	received := len(u.Chunks)
	var progress float64
	if u.TotalChunks > 0 {
		progress = float64(received) / float64(u.TotalChunks) * percentScale
	}
	bytes := int64(received) * int64(u.ChunkSize)
	if bytes > u.TotalSize {
		bytes = u.TotalSize
	}
	return TransferSummaryJSON{
		ID: u.ID, Name: path.Base(u.TargetPath), Path: u.TargetPath, TotalSize: u.TotalSize,
		ReceivedBytes: bytes, Progress: progress, Status: u.Status, DeviceID: u.DeviceID, UpdatedAt: u.UpdatedAt,
	}
}

// authorizeOverwrite re-checks, at each step of an upload session, that the caller may still replace the target.
// A session that does not overwrite needs no extra right.
func authorizeOverwrite(c *call, u *store.Upload) error {
	if !u.Overwrite {
		return nil
	}
	if d := authz.Decide(c.principal, authz.ActionUploadOverwrite, authz.Target{Paths: []string{u.TargetPath}}, c.policy); !d.Allowed {
		return d.Err
	}
	return nil
}
