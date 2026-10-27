package sidecar

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain doubles as a fake sidecar: the tests re-exec the test binary with RFE_FAKE_SIDECAR set.
func TestMain(m *testing.M) {
	if mode := os.Getenv("RFE_FAKE_SIDECAR"); mode != "" {
		fakeMain(mode)
		return
	}
	os.Exit(m.Run())
}

func fakeMain(mode string) {
	out := os.Stdout
	in := bufio.NewReader(os.Stdin)
	helloMsg := map[string]any{"kind": "hello", "proto": 1, "name": "fake", "version": "1.0.0"}
	switch mode {
	case "badproto":
		helloMsg["proto"] = 99
	case "badname":
		helloMsg["name"] = "other"
	case "silent":
		time.Sleep(time.Minute)
	}
	h, _ := json.Marshal(helloMsg)
	_ = writeFrame(out, kindJSON, h)
	var wmu sync.Mutex
	canceled := sync.Map{}
	for {
		f, err := readFrame(in)
		if err != nil {
			return
		}
		var env map[string]any
		_ = json.Unmarshal(f.payload, &env)
		id := uint64(env["id"].(float64))
		op, _ := env["op"].(string)
		reply := func(v map[string]any, body []byte) {
			v["id"] = id
			if body != nil {
				v["body"] = true
			}
			b, _ := json.Marshal(v)
			wmu.Lock()
			defer wmu.Unlock()
			_ = writeFrame(out, kindJSON, b)
			if body != nil {
				_ = writeFrame(out, kindBin, body)
			}
		}
		switch op {
		case "ping":
			reply(map[string]any{"ok": true}, nil)
		case "echo":
			reply(map[string]any{"ok": true, "text": env["text"]}, nil)
		case "fail":
			reply(map[string]any{"ok": false, "code": "BAD_REQUEST", "message": "nope"}, nil)
		case "body":
			reply(map[string]any{"ok": true}, []byte(strings.Repeat("x", 1<<20)))
		case "replyexit":
			reply(map[string]any{"ok": true, "text": "bye"}, nil)
			os.Exit(0)
		case "crash":
			os.Exit(3)
		case "cancel":
			canceled.Store(uint64(env["target"].(float64)), true)
			reply(map[string]any{"ok": true}, nil)
		case "slow":
			go func() {
				for i := 0; i < 200; i++ {
					if _, ok := canceled.Load(id); ok {
						reply(map[string]any{"ok": false, "code": "CANCELED", "message": "canceled"}, nil)
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
				reply(map[string]any{"ok": true}, nil)
			}()
		default:
			reply(map[string]any{"ok": false, "code": "NOT_SUPPORTED", "message": fmt.Sprint("op ", op)}, nil)
		}
	}
}

func fakeCfg(mode string) Config {
	exe, _ := os.Executable()
	return Config{Name: "fake", Path: exe, Env: []string{"RFE_FAKE_SIDECAR=" + mode}}
}
