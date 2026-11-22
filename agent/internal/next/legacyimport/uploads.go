package legacyimport

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	nstore "github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// transfersDir is the folder under the data directory that holds upload temp
// files; the new agent keeps using it with the same file names.
const transfersDir = "transfers"

// readUploads converts transfers and transfer_chunks to uploads and their
// chunk rows. Per session: the temp file name is the base name of the old
// temp_path (normally <id>.tmp); updated_at 0 becomes the import time (0
// would make the first sweep expire an open session); an unknown status
// becomes 'failed'; for an open session, chunk rows beyond what the temp file
// really holds are dropped, so those chunks are simply sent again.
func (im *importer) readUploads(db *sql.DB, dataDir string) ([]nstore.Upload, error) {
	ups, err := readTransferRows(db)
	if err != nil {
		return nil, err
	}
	chunks, err := readChunkRows(db)
	if err != nil {
		return nil, err
	}
	now := im.now()
	known := map[string]bool{}
	for i := range ups {
		u := &ups[i]
		known[u.ID] = true
		switch u.Status {
		case nstore.UploadOpen, nstore.UploadCompleted, nstore.UploadFailed:
		default:
			im.warnf("upload %s has unknown status %q; set to failed", u.ID, u.Status)
			u.Status = nstore.UploadFailed
		}
		if u.UpdatedAt == 0 {
			u.UpdatedAt = now
		}
		if u.Status != nstore.UploadOpen && u.TerminalAt == 0 {
			u.TerminalAt = now
		}
		u.Chunks = im.keepChunks(u, chunks[u.ID], tempSize(dataDir, u.TempName))
	}
	orphans := make([]string, 0)
	for id := range chunks {
		if !known[id] {
			orphans = append(orphans, id)
		}
	}
	sort.Strings(orphans)
	for _, id := range orphans {
		im.warnf("chunk rows for unknown upload %s dropped", id)
		im.rep.DroppedChunks += len(chunks[id])
	}
	return ups, nil
}

// readTransferRows reads the transfers table (temp_name already reduced to a
// bare file name). Status is passed through; the caller normalises it.
func readTransferRows(db *sql.DB) ([]nstore.Upload, error) {
	rows, err := db.Query(`SELECT id,target_path,total_size,chunk_size,sha256,status,temp_path,total_chunks,
COALESCE(device_id,''),COALESCE(updated_at,0),COALESCE(overwrite,0),COALESCE(terminal_at,0) FROM transfers ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read transfers: %w", err)
	}
	defer rows.Close()
	var out []nstore.Upload
	for rows.Next() {
		var u nstore.Upload
		var tempPath string
		var overwrite int
		if err := rows.Scan(&u.ID, &u.TargetPath, &u.TotalSize, &u.ChunkSize, &u.SHA256, &u.Status, &tempPath, &u.TotalChunks,
			&u.DeviceID, &u.UpdatedAt, &overwrite, &u.TerminalAt); err != nil {
			return nil, fmt.Errorf("read transfers: %w", err)
		}
		u.Overwrite = overwrite != 0
		u.TempName = tempName(tempPath)
		out = append(out, u)
	}
	return out, rows.Err()
}

// readChunkRows returns the received chunk numbers per upload id, ascending.
func readChunkRows(db *sql.DB) (map[string][]int, error) {
	rows, err := db.Query(`SELECT transfer_id,chunk_no FROM transfer_chunks ORDER BY transfer_id,chunk_no`)
	if err != nil {
		return nil, fmt.Errorf("read transfer_chunks: %w", err)
	}
	defer rows.Close()
	out := map[string][]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("read transfer_chunks: %w", err)
		}
		out[id] = append(out[id], n)
	}
	return out, rows.Err()
}

// keepChunks returns the chunk numbers of u to import. A number outside
// 0..total_chunks-1 is always dropped. For an open session a chunk is also
// dropped unless the temp file (tempBytes long) reaches the end of that chunk;
// finished sessions keep their rows as they were.
func (im *importer) keepChunks(u *nstore.Upload, rows []int, tempBytes int64) []int {
	var kept []int
	for _, n := range rows {
		if n < 0 || n >= u.TotalChunks {
			im.warnf("upload %s: chunk %d is outside 0..%d; dropped", u.ID, n, u.TotalChunks-1)
			im.rep.DroppedChunks++
			continue
		}
		if u.Status == nstore.UploadOpen && chunkEnd(u, n) > tempBytes {
			im.rep.DroppedChunks++
			continue
		}
		kept = append(kept, n)
	}
	if dropped := len(rows) - len(kept); dropped > 0 && u.Status == nstore.UploadOpen {
		im.warnf("upload %s: %d chunk row(s) beyond the %d bytes in its temp file dropped; the client resends them", u.ID, dropped, tempBytes)
	}
	return kept
}

// chunkEnd is the file offset just past chunk n: full chunks are chunk_size
// long, the last one ends at total_size. A chunk size of 0 or less cannot be
// placed, so it reports the end as beyond any file.
func chunkEnd(u *nstore.Upload, n int) int64 {
	if u.ChunkSize <= 0 {
		return u.TotalSize + 1
	}
	end := (int64(n) + 1) * int64(u.ChunkSize)
	if end > u.TotalSize {
		end = u.TotalSize
	}
	return end
}

// tempName reduces the old temp_path (an absolute path of the old machine or
// directory) to a bare file name, or "" if it has none.
func tempName(oldPath string) string {
	base := filepath.Base(oldPath)
	if oldPath == "" || base == "." || base == ".." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

// tempSize is the size of the regular file transfers/<name> under dataDir, or
// 0 if it is missing, not a regular file (a symlink is not followed) or unnamed.
func tempSize(dataDir, name string) int64 {
	if name == "" {
		return 0
	}
	fi, err := os.Lstat(filepath.Join(dataDir, transfersDir, name))
	if err != nil || !fi.Mode().IsRegular() {
		return 0
	}
	return fi.Size()
}
