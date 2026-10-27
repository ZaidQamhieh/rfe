// Package sidecar runs Rust helper processes (see protocol/sidecar.md) as children of the agent and talks to them
// over stdin/stdout. The agent never trusts a sidecar: every failure surfaces as an error the caller answers by
// falling back to its own implementation.
package sidecar

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	// ProtoVersion is the protocol version this agent speaks.
	ProtoVersion = 1

	kindJSON = 0
	kindBin  = 1

	// maxFrameBytes bounds any frame in either direction, so a corrupt length cannot make the agent allocate
	// without limit.
	maxFrameBytes = 160 << 20

	helloTimeout = 5 * time.Second
	closeGrace   = 2 * time.Second
)

// ErrUnavailable means no healthy sidecar could take the call; callers use their own implementation.
var ErrUnavailable = errors.New("sidecar unavailable")

// ErrBreakerOpen (always joined with ErrUnavailable) means repeated start failures opened the supervisor's breaker.
// A caller that parses untrusted input must not treat it as "the sidecar is simply absent" and decode in-process.
var ErrBreakerOpen = errors.New("sidecar breaker open")

// ErrInterrupted (always joined with ErrUnavailable) means the sidecar died or was killed while this request was
// in flight, as opposed to being down before the call. A caller that parses untrusted input uses it to tell "the
// sidecar is not running" from "this input may have crashed it".
var ErrInterrupted = errors.New("sidecar interrupted while handling the request")

// RemoteError is a failure the sidecar reported for one request.
type RemoteError struct {
	Code    string
	Message string
}

func (e *RemoteError) Error() string { return "sidecar " + e.Code + ": " + e.Message }

// Config describes one sidecar executable.
type Config struct {
	// Name is the name the sidecar must announce in its hello.
	Name string
	Path string
	Args []string
	// Env is added to the child's environment.
	Env []string
	// ExpectVersion, when set, must equal the version in the hello (a sidecar from another release is refused).
	ExpectVersion string
	// Logf receives the sidecar's stderr lines and lifecycle messages.
	Logf func(format string, args ...any)
}

func (c Config) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

type result struct {
	payload []byte
	body    []byte
	err     error
}

// Client is one running sidecar process.
type Client struct {
	cfg     Config
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	version string

	wmu sync.Mutex // one frame (or JSON+body pair) at a time

	mu      sync.Mutex
	pending map[uint64]chan result
	nextID  uint64
	failure error

	done chan struct{}
}

type frame struct {
	kind    byte
	payload []byte
}

func readFrame(r io.Reader) (frame, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return frame{}, err
	}
	n := binary.BigEndian.Uint32(lenBuf[:])
	if n == 0 || n > maxFrameBytes {
		return frame{}, fmt.Errorf("bad frame length %d", n)
	}
	// The sidecar is the less trusted side: grow with the bytes that arrive rather than allocating the declared
	// length up front.
	var buf bytes.Buffer
	buf.Grow(int(min(n, 1<<20)))
	if got, err := io.Copy(&buf, io.LimitReader(r, int64(n))); err != nil {
		return frame{}, err
	} else if got != int64(n) {
		return frame{}, io.ErrUnexpectedEOF
	}
	b := buf.Bytes()
	return frame{kind: b[0], payload: b[1:]}, nil
}

func writeFrame(w io.Writer, kind byte, payload []byte) error {
	if len(payload)+1 > maxFrameBytes {
		return errors.New("frame too large")
	}
	hdr := make([]byte, 5, 5+len(payload))
	binary.BigEndian.PutUint32(hdr, uint32(len(payload)+1))
	hdr[4] = kind
	_, err := w.Write(append(hdr, payload...))
	return err
}

type hello struct {
	Kind    string `json:"kind"`
	Proto   int    `json:"proto"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Start launches the sidecar and completes the handshake.
func Start(cfg Config) (*Client, error) {
	cmd := exec.Command(cfg.Path, cfg.Args...)
	cmd.Env = append(os.Environ(), cfg.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{cfg: cfg, cmd: cmd, stdin: stdin, pending: make(map[uint64]chan result), done: make(chan struct{})}
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			cfg.logf("sidecar %s: %s", cfg.Name, sc.Text())
		}
	}()

	br := bufio.NewReaderSize(stdout, 64<<10)
	type helloResult struct {
		h   hello
		err error
	}
	hc := make(chan helloResult, 1)
	go func() {
		f, err := readFrame(br)
		if err != nil {
			hc <- helloResult{err: err}
			return
		}
		var h hello
		if f.kind != kindJSON || json.Unmarshal(f.payload, &h) != nil {
			hc <- helloResult{err: errors.New("first frame is not a hello")}
			return
		}
		hc <- helloResult{h: h}
	}()
	select {
	case r := <-hc:
		if r.err == nil {
			r.err = c.checkHello(r.h)
		}
		if r.err != nil {
			c.kill()
			_ = cmd.Wait()
			return nil, fmt.Errorf("sidecar %s handshake: %w", cfg.Name, r.err)
		}
		c.version = r.h.Version
	case <-time.After(helloTimeout):
		c.kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("sidecar %s handshake: no hello within %v", cfg.Name, helloTimeout)
	}
	// Wait closes the stdout pipe, so it may only run once readLoop has drained it; otherwise a reply sent just
	// before the sidecar exits can be lost. readLoop ends on EOF, which the process exit produces.
	readDone := make(chan struct{})
	go func() {
		c.readLoop(br)
		close(readDone)
	}()
	go func() {
		<-readDone
		err := cmd.Wait()
		c.fail(fmt.Errorf("process exited: %v", err))
	}()
	return c, nil
}

func (c *Client) checkHello(h hello) error {
	switch {
	case h.Kind != "hello":
		return fmt.Errorf("unexpected first message kind %q", h.Kind)
	case h.Proto != ProtoVersion:
		return fmt.Errorf("protocol %d, agent speaks %d", h.Proto, ProtoVersion)
	case h.Name != c.cfg.Name:
		return fmt.Errorf("name %q, expected %q", h.Name, c.cfg.Name)
	case c.cfg.ExpectVersion != "" && h.Version != c.cfg.ExpectVersion:
		return fmt.Errorf("version %q, expected %q", h.Version, c.cfg.ExpectVersion)
	}
	return nil
}

// Version is the version the sidecar announced.
func (c *Client) Version() string { return c.version }

// Pid is the sidecar's process id (for diagnostics and tests).
func (c *Client) Pid() int { return c.cmd.Process.Pid }

// Done is closed when the sidecar is no longer usable.
func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) kill() {
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

// fail marks the client dead, kills the process and fails every waiting call.
func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.failure != nil {
		c.mu.Unlock()
		return
	}
	c.failure = err
	pending := c.pending
	c.pending = make(map[uint64]chan result)
	close(c.done)
	c.mu.Unlock()
	c.kill()
	_ = c.stdin.Close()
	for _, ch := range pending {
		ch <- result{err: fmt.Errorf("%w: %w: %v", ErrUnavailable, ErrInterrupted, err)}
	}
}

type respHead struct {
	ID      uint64 `json:"id"`
	OK      bool   `json:"ok"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Body    bool   `json:"body"`
}

func (c *Client) readLoop(br *bufio.Reader) {
	for {
		f, err := readFrame(br)
		if err != nil {
			c.fail(fmt.Errorf("read: %w", err))
			return
		}
		if f.kind != kindJSON {
			c.fail(errors.New("protocol violation: body frame without a message"))
			return
		}
		var head respHead
		if err := json.Unmarshal(f.payload, &head); err != nil {
			c.fail(fmt.Errorf("protocol violation: bad response: %w", err))
			return
		}
		var body []byte
		if head.Body {
			b, err := readFrame(br)
			if err != nil || b.kind != kindBin {
				c.fail(errors.New("protocol violation: missing body frame"))
				return
			}
			body = b.payload
		}
		c.mu.Lock()
		ch, ok := c.pending[head.ID]
		delete(c.pending, head.ID)
		c.mu.Unlock()
		if !ok {
			continue // a late answer to a canceled call, or the answer to a cancel
		}
		if !head.OK {
			ch <- result{err: &RemoteError{Code: head.Code, Message: head.Message}}
			continue
		}
		ch <- result{payload: f.payload, body: body}
	}
}

func merge(env, req []byte) []byte {
	if len(req) == 0 || bytes.Equal(req, []byte("null")) || bytes.Equal(req, []byte("{}")) {
		return env
	}
	// env and req are both JSON objects: splice the fields of req into env.
	out := make([]byte, 0, len(env)+len(req))
	out = append(out, env[:len(env)-1]...)
	out = append(out, ',')
	out = append(out, req[1:]...)
	return out
}

// Call sends one request and decodes the response fields into resp (which may be nil).
func (c *Client) Call(ctx context.Context, op string, req, resp any) error {
	_, err := c.call(ctx, op, req, nil, resp)
	return err
}

// CallBody is Call for operations that carry a binary body, returning the response body if there is one.
func (c *Client) CallBody(ctx context.Context, op string, req any, body []byte, resp any) ([]byte, error) {
	return c.call(ctx, op, req, body, resp)
}

func (c *Client) call(ctx context.Context, op string, req any, body []byte, resp any) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.failure != nil {
		c.mu.Unlock()
		return nil, ErrUnavailable
	}
	c.nextID++
	id := c.nextID
	ch := make(chan result, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	env := struct {
		ID   uint64 `json:"id"`
		Op   string `json:"op"`
		Body bool   `json:"body,omitempty"`
	}{ID: id, Op: op, Body: body != nil}
	envJSON, _ := json.Marshal(env)
	if err := c.send(merge(envJSON, reqJSON), body); err != nil {
		c.fail(fmt.Errorf("write: %w", err))
		return nil, fmt.Errorf("%w: %w: %v", ErrUnavailable, ErrInterrupted, err)
	}

	select {
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		if resp != nil {
			if err := json.Unmarshal(r.payload, resp); err != nil {
				return nil, fmt.Errorf("decode %s response: %w", op, err)
			}
		}
		return r.body, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.nextID++
		cancelID := c.nextID
		c.mu.Unlock()
		cancel, _ := json.Marshal(map[string]any{"id": cancelID, "op": "cancel", "target": id})
		_ = c.send(cancel, nil)
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, ErrInterrupted)
	}
}

// sendTimeout bounds one frame write. A sidecar that stops reading stdin would
// otherwise block the writer (and, through wmu, every other caller and the
// supervisor's ping) forever; on expiry the write fails and the caller tears
// the sidecar down.
var sendTimeout = 15 * time.Second

func (c *Client) send(msg, body []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if d, ok := c.stdin.(interface{ SetWriteDeadline(time.Time) error }); ok {
		_ = d.SetWriteDeadline(time.Now().Add(sendTimeout))
		defer func() { _ = d.SetWriteDeadline(time.Time{}) }()
	}
	if err := writeFrame(c.stdin, kindJSON, msg); err != nil {
		return err
	}
	if body != nil {
		return writeFrame(c.stdin, kindBin, body)
	}
	return nil
}

// Close ends the sidecar: stdin closes (which makes it exit) and it is killed if it lingers.
func (c *Client) Close() error {
	_ = c.stdin.Close()
	select {
	case <-c.done:
	case <-time.After(closeGrace):
		c.kill()
	}
	c.fail(errors.New("closed"))
	return nil
}
