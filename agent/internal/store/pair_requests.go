package store

import (
	"database/sql"
	"errors"
	"time"
)

// A pair request is a phone asking to be paired; the owner approves or rejects
// it at the PC. Statuses: committed (nonce hash only, not yet revealed),
// pending, approved (decided, token not yet collected), rejected, done (token
// collected).
const (
	// PairCommitted: the phone has sent only a hash of its nonce; the owner is not
	// asked until it reveals the nonce (see RevealPairRequest).
	PairCommitted = "committed"
	PairPending   = "pending"
	PairApproved  = "approved"
	PairRejected  = "rejected"
	PairDone      = "done"
)

// MaxPendingPairRequests bounds how many undecided requests can exist at once,
// so an unauthenticated peer cannot bury the owner in prompts.
const MaxPendingPairRequests = 3

// MaxCommittedPairRequestsPerIP and MaxCommittedPairRequests bound requests that
// have committed a nonce but not yet revealed it (the owner is not asked yet).
const (
	MaxCommittedPairRequestsPerIP = 3
	MaxCommittedPairRequests      = 30
)

// ErrTooManyPairRequests means MaxPendingPairRequests undecided requests already exist.
var ErrTooManyPairRequests = errors.New("too many pending pair requests")

// ErrPairRequestGone means the request does not exist, expired, or was already decided.
var ErrPairRequestGone = errors.New("pair request not found or already decided")

type PairRequest struct {
	ID          string
	Label       string
	ClientID    string
	PublicKey   string
	ClientNonce string // empty until revealed
	NonceCommit string // hex SHA-256 of the client nonce
	RemoteIP    string
	Status      string
	Created     int64
	Expires     int64
}

// pairRequestRetentionSecs is how long rejected and collected requests are kept before they are pruned.
const pairRequestRetentionSecs = 600

// CreatePairRequest stores a new pending request. Expired rows are pruned first.
func (s *DB) CreatePairRequest(r PairRequest) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().Unix()
	if _, err := tx.Exec(`DELETE FROM pair_requests WHERE expires < ? OR (status IN (?,?) AND created < ?)`, now, PairRejected, PairDone, now-pairRequestRetentionSecs); err != nil {
		return err
	}
	// Only requests the owner is actually being asked about count towards the
	// prompt cap. A committed request has not revealed its nonce yet, so it is
	// invisible to the owner; letting those fill the cap would let an
	// unauthenticated peer lock real phones out of pairing. They are bounded
	// separately, per source address and in total.
	var pending, committedTotal, committedFromIP int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pair_requests WHERE status=? AND expires >= ?`, PairPending, now).Scan(&pending); err != nil {
		return err
	}
	if pending >= MaxPendingPairRequests {
		return ErrTooManyPairRequests
	}
	if err := tx.QueryRow(`SELECT COUNT(*), COALESCE(SUM(remote_ip = ?), 0) FROM pair_requests WHERE status=? AND expires >= ?`, r.RemoteIP, PairCommitted, now).Scan(&committedTotal, &committedFromIP); err != nil {
		return err
	}
	if committedFromIP >= MaxCommittedPairRequestsPerIP || committedTotal >= MaxCommittedPairRequests {
		return ErrTooManyPairRequests
	}
	if _, err := tx.Exec(
		`INSERT INTO pair_requests (id,label,client_id,public_key,client_nonce,nonce_commit,remote_ip,status,created,expires) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Label, r.ClientID, r.PublicKey, "", r.NonceCommit, r.RemoteIP, PairCommitted, r.Created, r.Expires,
	); err != nil {
		return err
	}
	return tx.Commit()
}

const pairRequestCols = `id,label,client_id,public_key,client_nonce,nonce_commit,remote_ip,status,created,expires`

func scanPairRequest(row interface{ Scan(...any) error }) (PairRequest, error) {
	var r PairRequest
	err := row.Scan(&r.ID, &r.Label, &r.ClientID, &r.PublicKey, &r.ClientNonce, &r.NonceCommit, &r.RemoteIP, &r.Status, &r.Created, &r.Expires)
	return r, err
}

// GetPairRequest returns a request by id; ok is false when it is unknown or expired while undecided.
func (s *DB) GetPairRequest(id string) (PairRequest, bool, error) {
	r, err := scanPairRequest(s.db.QueryRow(`SELECT `+pairRequestCols+` FROM pair_requests WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return PairRequest{}, false, nil
	}
	if err != nil {
		return PairRequest{}, false, err
	}
	if r.Status == PairPending && r.Expires < time.Now().Unix() {
		return PairRequest{}, false, nil
	}
	return r, true, nil
}

// ListPendingPairRequests returns the undecided, unexpired requests, oldest first.
func (s *DB) ListPendingPairRequests() ([]PairRequest, error) {
	rows, err := s.db.Query(`SELECT `+pairRequestCols+` FROM pair_requests WHERE status=? AND expires >= ? ORDER BY created`, PairPending, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PairRequest
	for rows.Next() {
		r, err := scanPairRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DecidePairRequest approves or rejects a pending, unexpired request.
func (s *DB) DecidePairRequest(id string, approve bool) error {
	status := PairRejected
	if approve {
		status = PairApproved
	}
	res, err := s.db.Exec(`UPDATE pair_requests SET status=? WHERE id=? AND status=? AND expires >= ?`, status, id, PairPending, time.Now().Unix())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrPairRequestGone
	}
	return nil
}

// claimApprovedTx moves an approved, unexpired request to done inside tx and
// returns it; ok is false when it was not in that state (already collected,
// unknown, expired). The change takes effect only if the caller commits tx.
func claimApprovedTx(tx *sql.Tx, id string) (PairRequest, bool, error) {
	res, err := tx.Exec(`UPDATE pair_requests SET status=? WHERE id=? AND status=? AND expires >= ?`, PairDone, id, PairApproved, time.Now().Unix())
	if err != nil {
		return PairRequest{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return PairRequest{}, false, err
	}
	if n == 0 {
		return PairRequest{}, false, nil
	}
	r, err := scanPairRequest(tx.QueryRow(`SELECT `+pairRequestCols+` FROM pair_requests WHERE id=?`, id))
	if err != nil {
		return PairRequest{}, false, err
	}
	return r, true, nil
}

// ClaimApprovedPairRequest moves an approved request to done exactly once and
// returns it, so the device token is minted for one poll only. Prefer
// CompletePairRequest when a device must be written too: this alone spends the
// approval even if the caller then fails.
func (s *DB) ClaimApprovedPairRequest(id string) (PairRequest, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return PairRequest{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	r, ok, err := claimApprovedTx(tx, id)
	if err != nil || !ok {
		return PairRequest{}, false, err
	}
	return r, true, tx.Commit()
}

// CompletePairRequest spends an approved request and writes its device row
// (token stored hashed, access reset to browse-only when resetAccess is set)
// in one transaction, returning the request and the device id. If any step
// fails nothing is written, so the approval stays collectable on the next
// poll; ok is false when the request is not approved any more.
func (s *DB) CompletePairRequest(id, token string, resetAccess bool) (PairRequest, string, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return PairRequest{}, "", false, err
	}
	defer func() { _ = tx.Rollback() }()
	r, ok, err := claimApprovedTx(tx, id)
	if err != nil || !ok {
		return PairRequest{}, "", false, err
	}
	deviceID, err := upsertDeviceTx(tx, r.ClientID, r.Label, token, r.PublicKey, false)
	if err != nil {
		return PairRequest{}, "", false, err
	}
	if resetAccess {
		if err := resetDeviceAccessTx(tx, deviceID); err != nil {
			return PairRequest{}, "", false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return PairRequest{}, "", false, err
	}
	return r, deviceID, true, nil
}

// RevealPairRequest records the client nonce of a committed request and makes it
// pending, so the owner can be asked. It succeeds once, only while the request is
// still committed and unexpired; the caller has already checked the nonce against
// the commitment.
func (s *DB) RevealPairRequest(id, clientNonce string) (PairRequest, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return PairRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().Unix()
	// The owner prompt cap holds at reveal time too: committed requests were bounded separately, so without
	// this check many of them revealing together would put more prompts in front of the owner than the cap.
	var pending int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pair_requests WHERE status=? AND expires >= ?`, PairPending, now).Scan(&pending); err != nil {
		return PairRequest{}, err
	}
	if pending >= MaxPendingPairRequests {
		return PairRequest{}, ErrTooManyPairRequests
	}
	res, err := tx.Exec(`UPDATE pair_requests SET client_nonce=?, status=? WHERE id=? AND status=? AND expires >= ?`, clientNonce, PairPending, id, PairCommitted, now)
	if err != nil {
		return PairRequest{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return PairRequest{}, err
	}
	if n == 0 {
		return PairRequest{}, ErrPairRequestGone
	}
	r, err := scanPairRequest(tx.QueryRow(`SELECT `+pairRequestCols+` FROM pair_requests WHERE id=?`, id))
	if err != nil {
		return PairRequest{}, err
	}
	return r, tx.Commit()
}
