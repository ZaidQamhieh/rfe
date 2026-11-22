package hostupdate

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// maxRedirects is how many redirects a fetch follows, each one re-checked by checkURL.
const maxRedirects = 5

// newClient returns the HTTP client for the run: the injected one (copied, so its redirect policy can be set) or
// a default with TLS 1.2+ and timeouts on connecting and on waiting for response headers. Redirects are only
// followed to URLs checkURL accepts, so https can never be downgraded.
func (s *stager) newClient() *http.Client {
	var c http.Client
	if s.cfg.HTTPClient != nil {
		c = *s.cfg.HTTPClient
	} else {
		c = http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ForceAttemptHTTP2:     true,
		}}
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("%w: more than %d redirects", ErrDownload, maxRedirects)
		}
		return s.checkURL(req.URL.String())
	}
	return &c
}

// checkURL accepts an absolute https URL without embedded credentials. Plain http is accepted only when the caller
// injected its own client (a test) and the host is a loopback IP literal such as 127.0.0.1.
func (s *stager) checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return fmt.Errorf("%w: %q", ErrInsecureURL, raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if ip := net.ParseIP(u.Hostname()); s.cfg.HTTPClient != nil && ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrInsecureURL, raw)
}

// fetchSmall GETs a small document (manifest or signature) and returns at most limit bytes; a longer body fails.
func (s *stager) fetchSmall(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	if err := s.checkURL(rawURL); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, manifestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDownload, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s answered %s", ErrDownload, rawURL, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDownload, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrDownload, rawURL, limit)
	}
	return data, nil
}

// fileSHA256 returns the lowercase hex SHA-256 and the size of the file at p.
func fileSHA256(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// download makes part hold exactly the bundle: it continues a leftover .part with a Range request when one
// exists, stops at once if the server sends more than the manifest's size, and finally checks the size and
// SHA-256. A bad hash after a resume is retried once from scratch (the leftover may have been damaged); a bad
// hash of a fresh download is final.
func (s *stager) download(ctx context.Context, b Bundle, part string) error {
	resumed, err := s.fetchInto(ctx, b, part)
	if err != nil {
		return err
	}
	sum, n, err := fileSHA256(part)
	if err != nil {
		return err
	}
	if n == b.Size && strings.EqualFold(sum, b.SHA256) {
		return nil
	}
	if resumed {
		s.cfg.logf("resumed download failed its check; starting over")
		if err := os.Remove(part); err != nil {
			return err
		}
		if _, err := s.fetchInto(ctx, b, part); err != nil {
			return err
		}
		if sum, n, err = fileSHA256(part); err != nil {
			return err
		}
		if n == b.Size && strings.EqualFold(sum, b.SHA256) {
			return nil
		}
	}
	return fmt.Errorf("%w: got sha256 %s (%d bytes), manifest says %s", ErrHashMismatch, sum, n, b.SHA256)
}

// fetchInto appends the missing tail of the bundle to part (all of it when part is absent or unusable) and
// reports whether it continued an earlier partial file. It enforces the size limit while reading.
func (s *stager) fetchInto(ctx context.Context, b Bundle, part string) (resumed bool, err error) {
	have := int64(0)
	if st, err := os.Lstat(part); err == nil {
		if st.Mode().IsRegular() && st.Size() <= b.Size {
			have = st.Size()
		} else if err := os.Remove(part); err != nil {
			return false, err
		}
	}
	if have == b.Size {
		return have > 0, nil
	}
	if err := s.checkURL(b.URL); err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.URL, nil)
	if err != nil {
		return false, err
	}
	if have > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(have, 10)+"-")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrDownload, err)
	}
	defer resp.Body.Close()
	flags := os.O_WRONLY | os.O_CREATE
	switch {
	case resp.StatusCode == http.StatusPartialContent && have > 0:
		if !strings.HasPrefix(resp.Header.Get("Content-Range"), "bytes "+strconv.FormatInt(have, 10)+"-") {
			return false, fmt.Errorf("%w: server resumed at the wrong offset (%q)", ErrDownload, resp.Header.Get("Content-Range"))
		}
		flags |= os.O_APPEND
		resumed = true
	case resp.StatusCode == http.StatusOK:
		have = 0
		flags |= os.O_TRUNC
	default:
		return false, fmt.Errorf("%w: %s answered %s", ErrDownload, b.URL, resp.Status)
	}
	want := b.Size - have
	if resp.ContentLength > want {
		return false, fmt.Errorf("%w: server announces %d bytes, expected %d", ErrTooLarge, resp.ContentLength, want)
	}
	out, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return false, err
	}
	n, err := io.Copy(out, io.LimitReader(resp.Body, want+1))
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	switch {
	case n > want:
		return resumed, fmt.Errorf("%w: server sent more than %d bytes", ErrTooLarge, want)
	case errors.Is(err, io.ErrUnexpectedEOF) || (err == nil && n < want):
		return resumed, fmt.Errorf("%w: got %d of %d bytes", ErrTruncated, have+n, b.Size)
	case err != nil:
		return resumed, fmt.Errorf("%w: %v", ErrDownload, err)
	}
	return resumed, nil
}
