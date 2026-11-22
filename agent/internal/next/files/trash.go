package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// The trash follows the XDG layout of the old agent, so items already in a
// trash stay restorable: files/ holds the trashed items under a unique name
// (the id) and info/ holds one "<id>.trashinfo" sidecar per item with its
// original path and deletion time.
const (
	trashFilesDir   = "files"
	trashInfoDir    = "info"
	trashInfoExt    = ".trashinfo"
	trashInfoHeader = "[Trash Info]"
	trashPathKey    = "Path="
	trashDateKey    = "DeletionDate="
	// xdgDeletionDate is the DeletionDate layout of the spec (local time, no zone).
	xdgDeletionDate = "2006-01-02T15:04:05"
	// trashInfoMaxBytes bounds how much of a sidecar is read.
	trashInfoMaxBytes = 64 << 10
	trashInfoPerm     = fs.FileMode(0o600)
	trashDirPerm      = fs.FileMode(0o700)
)

// ErrBadTrashID is returned for a trash id that is not a single opaque name.
var ErrBadTrashID = fmt.Errorf("%w: invalid trash id", api.ErrBadRequest)

// ErrTrashNotConfigured is returned by the trash operations of a Service that
// was never given a trash folder (WithTrash).
var ErrTrashNotConfigured = fmt.Errorf("%w: the trash is not set up", api.ErrNotImplemented)

// TrashEntry is one item in the trash (openapi schema TrashEntry).
type TrashEntry struct {
	ID           string    `json:"id"` // unique name under files/
	Name         string    `json:"name"`
	OriginalPath string    `json:"originalPath"`
	DeletedAt    time.Time `json:"deletedAt"`
	Size         int64     `json:"size"`
	IsDir        bool      `json:"isDir"`
}

// trashStore is the trash folder, reached through its own Set so that it may lie
// outside the allowed folders or inside the protected data folder.
type trashStore struct {
	dir string
	set *rootfs.Set
	svc *Service
	now func() time.Time
}

// WithTrash gives the Service its trash folder dir (an absolute path, created
// with files/ and info/ if missing). Call it once before the Service is shared;
// a second call replaces the folder. Close releases it.
func (s *Service) WithTrash(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("%w: the trash folder must be an absolute path", api.ErrBadRequest)
	}
	if err := os.MkdirAll(dir, trashDirPerm); err != nil {
		return err
	}
	set, err := rootfs.New([]string{dir}, rootfs.Options{})
	if err != nil {
		return err
	}
	svc := New(set)
	for _, sub := range []string{trashFilesDir, trashInfoDir} {
		h, err := svc.open(filepath.Join(dir, sub))
		if err == nil {
			err = classify(h.MkdirAll(trashDirPerm))
		}
		if err != nil {
			_ = set.Close()
			return err
		}
	}
	s.trash = &trashStore{dir: dir, set: set, svc: svc, now: time.Now}
	return nil
}

// Close releases the trash folder opened by WithTrash. It never closes the Set
// the Service works in; that belongs to its creator.
func (s *Service) Close() error {
	if s.trash == nil {
		return nil
	}
	err := s.trash.set.Close()
	s.trash = nil
	return err
}

// ValidTrashID reports whether id is one opaque name: not empty, "." or "..",
// with no separator, volume prefix or NUL, so it cannot reach outside files/.
func ValidTrashID(id string) bool {
	switch {
	case id == "" || id == "." || id == "..":
		return false
	case strings.ContainsAny(id, "/\\\x00"):
		return false
	}
	return id == filepath.Base(id) && !filepath.IsAbs(id) && filepath.VolumeName(id) == ""
}

// filesPath and infoPath are the places of an item's payload and sidecar.
func (t *trashStore) filesPath(id string) string { return filepath.Join(t.dir, trashFilesDir, id) }
func (t *trashStore) infoPath(id string) string {
	return filepath.Join(t.dir, trashInfoDir, id+trashInfoExt)
}

// sees reports whether orig lies inside the folders this Service may reach, so
// a jailed device never learns of items deleted elsewhere.
func (s *Service) sees(orig string) bool {
	_, err := s.set.Open(orig)
	return err == nil
}

// trashCandidate is the i-th name tried for an item called name: the name
// itself, then "stem_1.ext", "stem_2.ext" (the old agent's scheme).
func trashCandidate(name string, i int) string {
	if i == 0 {
		return name
	}
	ext := filepath.Ext(name)
	return fmt.Sprintf("%s_%d%s", strings.TrimSuffix(name, ext), i, ext)
}

// encodeTrashPath percent-encodes each segment of an absolute path the XDG way
// and keeps the "/" separators literal, so desktop file managers can parse it.
func encodeTrashPath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}

// readInfo reads the sidecar of id: the original absolute path and the deletion
// time. A missing or malformed sidecar is an error.
func (t *trashStore) readInfo(id string) (orig string, deleted time.Time, err error) {
	h, err := t.svc.open(t.infoPath(id))
	if err != nil {
		return "", time.Time{}, err
	}
	f, err := h.Open()
	if err != nil {
		return "", time.Time{}, classify(err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, trashInfoMaxBytes))
	if err != nil {
		return "", time.Time{}, classify(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, trashPathKey); ok {
			dec, derr := url.PathUnescape(strings.TrimSpace(v))
			if derr != nil {
				return "", time.Time{}, fmt.Errorf("%w: malformed trash info", api.ErrBadRequest)
			}
			orig = dec
		} else if v, ok := strings.CutPrefix(line, trashDateKey); ok {
			deleted, _ = time.ParseInLocation(xdgDeletionDate, strings.TrimSpace(v), time.Local)
		}
	}
	if orig == "" || !filepath.IsAbs(orig) {
		return "", time.Time{}, fmt.Errorf("%w: malformed trash info", api.ErrBadRequest)
	}
	return orig, deleted, nil
}

// reserveID picks a free id for an item called name and writes its sidecar. The
// sidecar is created exclusively before the payload moves, so two deletions
// never share an id and a crash leaves a listed (not an invisible) item.
func (t *trashStore) reserveID(name, orig string) (string, *rootfs.Handle, error) {
	content := trashInfoHeader + "\n" + trashPathKey + encodeTrashPath(orig) + "\n" +
		trashDateKey + t.now().Format(xdgDeletionDate) + "\n"
	for i := 0; i <= maxAutoRename; i++ {
		id := trashCandidate(name, i)
		infoH, err := t.svc.open(t.infoPath(id))
		if err != nil {
			return "", nil, err
		}
		f, err := infoH.OpenFile(os.O_WRONLY|os.O_CREATE|os.O_EXCL, trashInfoPerm)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, classify(err)
		}
		payload, err := t.svc.open(t.filesPath(id))
		if err == nil {
			var found bool
			_, found, err = probe(payload)
			if err == nil && found {
				_ = f.Close()
				_ = infoH.Remove()
				continue
			}
		}
		if err == nil {
			_, err = io.WriteString(f, content)
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return "", nil, t.svc.discard(infoH, err)
		}
		return id, infoH, nil
	}
	return "", nil, fmt.Errorf("%w: no free trash name left", api.ErrConflict)
}

// Trash moves each path into the trash and returns one result per path. The
// item can be put back with Restore. A move across devices or allowed folders
// is a copy followed by removing the source, because rootfs has no rename
// between two folder sets. A symbolic link itself cannot be trashed
// (ErrSymlinkCopy); a link inside a trashed folder is kept when it stays inside.
func (s *Service) Trash(paths []string) []api.ItemResult {
	results := make([]api.ItemResult, len(paths))
	for i, p := range paths {
		results[i] = api.Item(p, s.trashOne(p))
	}
	return results
}

// trashOne trashes a single path; see Trash.
func (s *Service) trashOne(path string) error {
	if s.set.IsReadOnly() {
		return authz.ErrReadOnly
	}
	t := s.trash
	if t == nil {
		return ErrTrashNotConfigured
	}
	h, err := s.open(path)
	if err != nil {
		return err
	}
	name, err := entryName(h.Path())
	if err != nil {
		return err
	}
	info, err := entryInfo(h)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return ErrSymlinkCopy
	}
	if err := h.CheckTree(); err != nil {
		return classify(err)
	}
	id, infoH, err := t.reserveID(name, h.Path())
	if err != nil {
		return err
	}
	if err := s.moveIntoTrash(t, h, info, id); err != nil {
		if !errors.Is(err, ErrMoveIncomplete) {
			err = t.svc.discard(infoH, err)
		}
		return err
	}
	return nil
}

// moveIntoTrash copies h to a hidden name in files/, renames it to id and then
// removes the source. If the source cannot be removed the trash copy is kept
// and ErrMoveIncomplete is returned.
func (s *Service) moveIntoTrash(t *trashStore, h *rootfs.Handle, info fs.FileInfo, id string) error {
	filesDir := filepath.Join(t.dir, trashFilesDir)
	removeErr, err := copyThenRemove(s, t.svc, h, info, filesDir, func() (*rootfs.Handle, error) { return t.svc.open(t.filesPath(id)) })
	if err != nil {
		return err
	}
	if removeErr != nil {
		return fmt.Errorf("%w: %w", ErrMoveIncomplete, removeErr)
	}
	return nil
}

// ListTrash lists the trashed items whose original path lies inside this
// Service's folders, newest deletion first. A trash with no sidecars is empty;
// a sidecar that cannot be read is left out rather than failing the list.
func (s *Service) ListTrash() ([]TrashEntry, error) {
	t := s.trash
	if t == nil {
		return nil, ErrTrashNotConfigured
	}
	dir, err := t.svc.open(filepath.Join(t.dir, trashInfoDir))
	if err != nil {
		return nil, err
	}
	des, err := dir.ReadDir()
	if isMissing(err) {
		return []TrashEntry{}, nil
	}
	if err != nil {
		return nil, classify(err)
	}
	out := make([]TrashEntry, 0, len(des))
	for _, de := range des {
		id, ok := strings.CutSuffix(de.Name(), trashInfoExt)
		if !ok || de.IsDir() || !ValidTrashID(id) {
			continue
		}
		orig, deleted, err := t.readInfo(id)
		if err != nil || !s.sees(orig) {
			continue
		}
		e := TrashEntry{ID: id, Name: filepath.Base(orig), OriginalPath: orig, DeletedAt: deleted}
		if ph, err := t.svc.open(t.filesPath(id)); err == nil {
			if fi, err := ph.Lstat(); err == nil {
				e.Size, e.IsDir = fi.Size(), fi.IsDir()
			}
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].DeletedAt.Equal(out[j].DeletedAt) {
			return out[i].DeletedAt.After(out[j].DeletedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Restore moves each id back to the path recorded in its sidecar and returns one
// result per id: Path is the restored location on success, else the id. A name
// that is taken again is never replaced; the item comes back as "name (N)".
// Missing parent folders are created.
func (s *Service) Restore(ids []string) []api.ItemResult {
	results := make([]api.ItemResult, len(ids))
	for i, id := range ids {
		dest, err := s.restoreOne(id)
		if err == nil {
			results[i] = api.ItemResult{Path: dest, OK: true}
			continue
		}
		results[i] = api.Item(id, err)
	}
	return results
}

// restoreOne restores one id and returns where it went; see Restore.
func (s *Service) restoreOne(id string) (string, error) {
	if s.set.IsReadOnly() {
		return "", authz.ErrReadOnly
	}
	t := s.trash
	if t == nil {
		return "", ErrTrashNotConfigured
	}
	if !ValidTrashID(id) {
		return "", ErrBadTrashID
	}
	orig, _, err := t.readInfo(id)
	if err != nil {
		return "", &opError{kind: api.ErrPathNotFound, cause: err}
	}
	want, err := s.open(orig)
	if err != nil {
		return "", err
	}
	if _, err := entryName(want.Path()); err != nil {
		return "", err
	}
	payload, err := t.svc.open(t.filesPath(id))
	if err != nil {
		return "", err
	}
	info, err := entryInfo(payload)
	if err != nil {
		return "", err
	}
	final := want
	if _, found, err := probe(want); err != nil {
		return "", err
	} else if found {
		if final, err = s.autoName(want); err != nil {
			return "", err
		}
	}
	if err := s.ensureParent(final); err != nil {
		return "", err
	}
	removeErr, err := copyThenRemove(t.svc, s, payload, info, filepath.Dir(final.Path()), func() (*rootfs.Handle, error) { return final, nil })
	if err != nil {
		return "", err
	}
	if removeErr != nil {
		return "", fmt.Errorf("%w: restored, but the trash copy remains: %w", ErrMoveIncomplete, removeErr)
	}
	infoH, err := t.svc.open(t.infoPath(id))
	if err == nil {
		err = infoH.Remove()
	}
	if err != nil && !isMissing(err) {
		return "", fmt.Errorf("%w: restored, but its trash record remains: %w", ErrMoveIncomplete, err)
	}
	return final.Path(), nil
}

// EmptyTrash permanently removes the given trash ids, or every visible item
// when ids is empty, and returns one result per id. An id that is unknown, or
// whose original path lies outside this Service's folders, is a failed item
// with api.ErrPathNotFound; the other ids are still processed.
func (s *Service) EmptyTrash(ids []string) ([]api.ItemResult, error) {
	if s.set.IsReadOnly() {
		return nil, authz.ErrReadOnly
	}
	if s.trash == nil {
		return nil, ErrTrashNotConfigured
	}
	if len(ids) == 0 {
		all, err := s.ListTrash()
		if err != nil {
			return nil, err
		}
		for _, e := range all {
			ids = append(ids, e.ID)
		}
	}
	results := make([]api.ItemResult, len(ids))
	for i, id := range ids {
		results[i] = api.Item(id, s.emptyOne(id))
	}
	return results, nil
}

// emptyOne removes one trash item (payload first, then its sidecar).
func (s *Service) emptyOne(id string) error {
	t := s.trash
	if !ValidTrashID(id) {
		return ErrBadTrashID
	}
	orig, _, err := t.readInfo(id)
	if err != nil || !s.sees(orig) {
		return fmt.Errorf("%w: no such trash item", api.ErrPathNotFound)
	}
	payload, err := t.svc.open(t.filesPath(id))
	if err != nil {
		return err
	}
	if _, found, err := probe(payload); err != nil {
		return err
	} else if found {
		if err := payload.RemoveAll(); err != nil {
			return classify(err)
		}
	}
	infoH, err := t.svc.open(t.infoPath(id))
	if err != nil {
		return err
	}
	if err := infoH.Remove(); err != nil && !isMissing(err) {
		return classify(err)
	}
	return nil
}
