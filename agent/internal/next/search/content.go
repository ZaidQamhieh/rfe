package search

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/files"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// Content search limits. A file larger than MaxContentFileBytes is not read;
// a file whose first ContentSniffBytes hold a NUL byte is binary and skipped.
// Files are read in ContentChunkBytes pieces.
const (
	MaxContentFileBytes = 8 << 20
	ContentSniffBytes   = 512
	ContentChunkBytes   = 64 << 10
	// SnippetMaxBytes bounds the text of a snippet (cut at rune boundaries).
	SnippetMaxBytes = 160
)

// fileMatch reports whether the file at h contains needle (already
// lowercase) ignoring case, and returns the line of the first match as a
// snippet (nil when the line cannot be located, never a reason to drop the hit). The file is read through the jailed handle in
// chunks, so memory stays bounded; the end of each chunk is carried into the
// next one so a match across a chunk boundary is found. Binary files, and
// anything after MaxContentFileBytes, are not searched.
func fileMatch(ctx context.Context, h *rootfs.Handle, needle []byte) (*files.Snippet, bool, error) {
	f, err := h.Open()
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	chunk := make([]byte, ContentChunkBytes)
	// carry is the raw tail of the previous window. Lowering can change the
	// byte length of a rune, so a few extra bytes are kept beyond the needle.
	keep := len(needle) + utf8.UTFMax
	var carry []byte
	total := 0
	first := true
	for total < MaxContentFileBytes {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		n, err := f.Read(chunk)
		if n > 0 {
			total += n
			if first {
				first = false
				if bytes.IndexByte(chunk[:min(n, ContentSniffBytes)], 0) >= 0 {
					return nil, false, nil
				}
			}
			window := append(carry, chunk[:n]...)
			lower := bytes.ToLower(window)
			if i := bytes.Index(lower, needle); i >= 0 {
				return snippetAt(window, lower, i, len(needle)), true, nil
			}
			carry = append([]byte(nil), window[max(0, len(window)-keep):]...)
		}
		if errors.Is(err, io.EOF) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
	}
	return nil, false, nil
}

// snippetAt cuts the line around the match at lower[at:at+n] out of window.
// Lowering can change byte lengths; then the offsets do not map and there is
// no snippet.
func snippetAt(window, lower []byte, at, n int) *files.Snippet {
	if len(window) != len(lower) {
		return nil
	}
	start := bytes.LastIndexByte(window[:at], '\n') + 1
	end := len(window)
	if j := bytes.IndexByte(window[at:], '\n'); j >= 0 {
		end = at + j
	}
	// Keep the match, then spend what is left of the budget on both sides.
	mEnd := min(at+n, end)
	if mEnd-at > SnippetMaxBytes {
		mEnd = at + SnippetMaxBytes
	}
	spare := (SnippetMaxBytes - (mEnd - at)) / 2
	from, to := max(start, at-spare), min(end, mEnd+spare)
	for from < at && !utf8.RuneStart(window[from]) {
		from++
	}
	for to > mEnd && to < len(window) && !utf8.RuneStart(window[to]) {
		to--
	}
	clean := func(b []byte) string { return strings.ToValidUTF8(string(b), "\uFFFD") }
	before, match := clean(window[from:at]), clean(window[at:mEnd])
	text := strings.TrimRight(before+match+clean(window[mEnd:to]), "\r")
	return &files.Snippet{Text: text, Start: utf16Len(before), Length: utf16Len(match)}
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}
