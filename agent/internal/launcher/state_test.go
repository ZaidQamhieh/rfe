package launcher

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func sameState(a, b State) bool {
	x, _ := json.Marshal(a.clone())
	y, _ := json.Marshal(b.clone())
	return string(x) == string(y)
}

func TestNextTable(t *testing.T) {
	base := State{Current: "v1", Previous: "v0", Attempts: map[string]int{}}
	cases := []struct {
		name  string
		from  State
		ev    Event
		want  State
		isErr error
	}{
		{"set current on fresh", State{}, Event{EvSetCurrent, "v1", t0}, State{Current: "v1", Updated: t0}, nil},
		{"set current repeat", State{Current: "v1"}, Event{EvSetCurrent, "v1", t0}, State{Current: "v1", Updated: t0}, nil},
		{"set current replaces nothing", State{Current: "v1"}, Event{EvSetCurrent, "v2", t0}, State{}, ErrTransition},
		{"set current bad name", State{}, Event{EvSetCurrent, "../x", t0}, State{}, ErrTransition},
		{"set pending", base, Event{EvSetPending, "v2", t0}, State{Current: "v1", Previous: "v0", Pending: "v2", Updated: t0}, nil},
		{"set pending resets attempts", State{Current: "v1", Attempts: map[string]int{"v2": 1}}, Event{EvSetPending, "v2", t0}, State{Current: "v1", Pending: "v2", Updated: t0}, nil},
		{"set pending same as current", base, Event{EvSetPending, "v1", t0}, State{}, ErrTransition},
		{"set pending bad version", State{Current: "v1", Bad: []string{"v2"}}, Event{EvSetPending, "v2", t0}, State{}, ErrTransition},
		{"set pending without current", State{}, Event{EvSetPending, "v2", t0}, State{}, ErrTransition},
		{"promote", State{Current: "v1", Previous: "v0", Pending: "v2", Attempts: map[string]int{"v2": 1}}, Event{EvPromote, "", t0}, State{Current: "v2", Previous: "v1", Updated: t0}, nil},
		{"promote nothing", base, Event{EvPromote, "", t0}, State{}, ErrTransition},
		{"first failure keeps pending", State{Current: "v1", Pending: "v2"}, Event{EvFailPending, "v2", t0}, State{Current: "v1", Pending: "v2", Attempts: map[string]int{"v2": 1}, Updated: t0}, nil},
		{"second failure marks bad and reverts", State{Current: "v1", Pending: "v2", Attempts: map[string]int{"v2": 1}}, Event{EvFailPending, "v2", t0}, State{Current: "v1", Attempts: map[string]int{"v2": 2}, Bad: []string{"v2"}, Updated: t0}, nil},
		{"fail pending wrong version", State{Current: "v1", Pending: "v2"}, Event{EvFailPending, "v3", t0}, State{}, ErrTransition},
		{"fail pending none", base, Event{EvFailPending, "", t0}, State{}, ErrTransition},
		{"mark bad pending", State{Current: "v1", Pending: "v2"}, Event{EvMarkBad, "v2", t0}, State{Current: "v1", Bad: []string{"v2"}, Updated: t0}, nil},
		{"mark bad current falls back", State{Current: "v2", Previous: "v1"}, Event{EvMarkBad, "v2", t0}, State{Current: "v1", Bad: []string{"v2"}, Updated: t0}, nil},
		{"mark bad current without previous", State{Current: "v2"}, Event{EvMarkBad, "v2", t0}, State{}, ErrNoFallback},
		{"mark bad previous", State{Current: "v2", Previous: "v1"}, Event{EvMarkBad, "v1", t0}, State{Current: "v2", Bad: []string{"v1"}, Updated: t0}, nil},
		{"mark bad twice lists once", State{Current: "v1", Bad: []string{"v2"}}, Event{EvMarkBad, "v2", t0}, State{Current: "v1", Bad: []string{"v2"}, Updated: t0}, nil},
		{"unknown event", base, Event{"explode", "v1", t0}, State{}, ErrTransition},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before, _ := json.Marshal(c.from)
			got, err := Next(c.from, c.ev)
			after, _ := json.Marshal(c.from)
			if string(before) != string(after) {
				t.Fatalf("Next changed its input: %s -> %s", before, after)
			}
			if c.isErr != nil {
				if !errors.Is(err, c.isErr) {
					t.Fatalf("err = %v, want %v", err, c.isErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !sameState(got, c.want) {
				g, _ := json.Marshal(got)
				w, _ := json.Marshal(c.want.clone())
				t.Fatalf("got %s\nwant %s", g, w)
			}
		})
	}
}

func TestPick(t *testing.T) {
	for _, c := range []struct {
		s       State
		want    string
		pending bool
	}{
		{State{Current: "v1"}, "v1", false},
		{State{Current: "v1", Pending: "v2"}, "v2", true},
		{State{Current: "v1", Pending: "v2", Attempts: map[string]int{"v2": 1}}, "v2", true},
		{State{Current: "v1", Pending: "v2", Attempts: map[string]int{"v2": 2}}, "v1", false},
		{State{Current: "v1", Pending: "v2", Bad: []string{"v2"}}, "v1", false},
	} {
		if v, p := Pick(c.s); v != c.want || p != c.pending {
			t.Errorf("Pick(%+v) = %s, %v", c.s, v, p)
		}
	}
}

func TestReadFreshAndCorrupt(t *testing.T) {
	st := NewStore(t.TempDir())
	got, err := st.Read()
	if err != nil || got.Current != "" || got.Attempts == nil {
		t.Fatalf("fresh read = %+v, %v", got, err)
	}
	good := State{Current: "v1", Attempts: map[string]int{}, Updated: t0}
	if err := st.Write(good); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Root, StateFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = st.Read()
	if !errors.Is(err, ErrCorruptState) || !strings.Contains(err.Error(), BackupFile) {
		t.Fatalf("corrupt read error = %v", err)
	}
	// The backup still holds the last good state, byte for byte.
	bak, err := os.ReadFile(filepath.Join(st.Root, BackupFile))
	if err != nil {
		t.Fatal(err)
	}
	var back State
	if err := json.Unmarshal(bak, &back); err != nil || back.Current != "v1" {
		t.Fatalf("backup = %q, %v", bak, err)
	}
}

func TestReadRejectsUnsafeVersionName(t *testing.T) {
	st := NewStore(t.TempDir())
	os.WriteFile(filepath.Join(st.Root, StateFile), []byte(`{"current":"../../etc"}`), 0o600)
	if _, err := st.Read(); !errors.Is(err, ErrCorruptState) {
		t.Fatalf("err = %v", err)
	}
	if err := st.Write(State{Current: "a/b"}); !errors.Is(err, ErrCorruptState) {
		t.Fatalf("write err = %v", err)
	}
}

func TestWriteRefusesSymlinkedState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	st := NewStore(t.TempDir())
	target := filepath.Join(t.TempDir(), "elsewhere")
	os.WriteFile(target, []byte("{}"), 0o600)
	if err := os.Symlink(target, filepath.Join(st.Root, StateFile)); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(State{Current: "v1"}); err == nil {
		t.Fatal("Write followed a symlink")
	}
	if _, err := st.Read(); !errors.Is(err, ErrCorruptState) {
		t.Fatalf("Read of a symlink: %v", err)
	}
}

type simulatedKill struct{ step string }

// TestWriteKilledAtEveryStep stops a write before each durable step (panic, as a kill would, leaving the temp
// file behind) and checks the root still reads as the complete old or the complete new state, and the backup is a
// complete state too. Then it checks an error at the same point (no panic) leaves no temp file.
func TestWriteKilledAtEveryStep(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a killed child still holds its temp file on Windows, so the temp dir cannot be removed")
	}
	old := State{Current: "v1", Attempts: map[string]int{}, Bad: []string{}, Updated: t0}
	next := State{Current: "v1", Pending: "v2", Attempts: map[string]int{}, Bad: []string{}, Updated: t0.Add(time.Hour)}

	var steps []string
	st := NewStore(t.TempDir())
	st.hook = func(s string) error { steps = append(steps, s); return nil }
	if err := st.Write(next); err != nil {
		t.Fatal(err)
	}
	if len(steps) != 8 {
		t.Fatalf("steps = %v", steps)
	}

	for k, step := range steps {
		for _, mode := range []string{"panic", "error"} {
			t.Run(step+"/"+mode, func(t *testing.T) {
				st := NewStore(t.TempDir())
				if err := st.Write(old); err != nil {
					t.Fatal(err)
				}
				calls := 0
				st.hook = func(s string) error {
					if calls++; calls == k+1 {
						if mode == "panic" {
							panic(simulatedKill{s})
						}
						return errors.New("injected failure at " + s)
					}
					return nil
				}
				func() {
					defer func() {
						if r := recover(); r != nil {
							if _, ok := r.(simulatedKill); !ok {
								panic(r)
							}
						}
					}()
					if err := st.Write(next); err == nil {
						t.Fatal("write did not fail")
					}
				}()
				st.hook = nil
				got, err := st.Read()
				if err != nil {
					t.Fatalf("read after kill: %v", err)
				}
				if !sameState(got, old) && !sameState(got, next) {
					t.Fatalf("partial state: %+v", got)
				}
				for _, name := range []string{StateFile, BackupFile} {
					data, err := os.ReadFile(filepath.Join(st.Root, name))
					var s State
					if err != nil || json.Unmarshal(data, &s) != nil || (!sameState(s, old) && !sameState(s, next)) {
						t.Fatalf("%s after kill at %s: %q %v", name, step, data, err)
					}
				}
				if mode == "error" {
					if left, _ := filepath.Glob(filepath.Join(st.Root, "*.tmp")); len(left) != 0 {
						t.Fatalf("temp files left after an error: %v", left)
					}
				}
				// The next clean write recovers whatever the kill left.
				if err := st.Write(next); err != nil {
					t.Fatal(err)
				}
				if got, _ := st.Read(); !sameState(got, next) {
					t.Fatalf("after recovery: %+v", got)
				}
			})
		}
	}
}

func TestApplyWritesBackupEachTime(t *testing.T) {
	st := NewStore(t.TempDir())
	st.now = func() time.Time { return t0 }
	if _, err := st.Apply(EvSetCurrent, "v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Apply(EvSetPending, "v2"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Apply(EvPromote, ""); err != nil {
		t.Fatal(err)
	}
	bak, _ := os.ReadFile(filepath.Join(st.Root, BackupFile))
	main, _ := os.ReadFile(filepath.Join(st.Root, StateFile))
	if string(bak) != string(main) || !strings.Contains(string(main), `"current": "v2"`) {
		t.Fatalf("state %s\nbackup %s", main, bak)
	}
}
