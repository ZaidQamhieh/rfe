package thumbs

import "sync"

// Crash log bounds.
const (
	// crashLogCap is how many file versions the crash log remembers.
	crashLogCap = 256
	// poisonStrikes is the number of renderer interruptions on one file
	// version after which the file is no longer sent to the renderer.
	poisonStrikes = 2
)

// crashKey identifies a file version: a changed file gets a fresh chance.
type crashKey struct {
	path  string
	size  int64
	mod   int64
	ident string
}

// crashLog remembers (bounded, oldest out first) the file versions a renderer
// was interrupted on. Other renders in flight when the renderer dies are
// interrupted too, which is why one strike does not condemn a file.
type crashLog struct {
	mu      sync.Mutex
	strikes map[crashKey]int
	order   []crashKey
}

// struck reports whether the renderer was interrupted on this version at least once.
func (l *crashLog) struck(k crashKey) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.strikes[k] >= 1
}

// poisoned reports whether this version has used up its strikes.
func (l *crashLog) poisoned(k crashKey) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.strikes[k] >= poisonStrikes
}

// strike records one interruption and reports whether the version is now poisoned.
func (l *crashLog) strike(k crashKey) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.strikes == nil {
		l.strikes = make(map[crashKey]int)
	}
	if _, seen := l.strikes[k]; !seen {
		l.order = append(l.order, k)
		if len(l.order) > crashLogCap {
			delete(l.strikes, l.order[0])
			l.order = l.order[1:]
		}
	}
	l.strikes[k]++
	return l.strikes[k] >= poisonStrikes
}
