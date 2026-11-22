package ratelimit

import (
	"testing"
	"time"
)

const (
	testAttempts = 2
	testOneShot  = 1
	testWindow   = time.Minute
	testMaxKeys  = 2
)

var testStart = time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)

func TestAllowUpToLimitThenRefuse(t *testing.T) {
	l := New(testAttempts, testWindow, testMaxKeys)
	for i := 0; i < testAttempts; i++ {
		if !l.Allow("a", testStart) {
			t.Fatalf("request %d within the budget was refused", i+1)
		}
	}
	if l.Allow("a", testStart) {
		t.Fatal("a request beyond the budget was admitted")
	}
}

func TestBudgetResetsAfterWindow(t *testing.T) {
	l := New(testOneShot, testWindow, testMaxKeys)
	if !l.Allow("a", testStart) {
		t.Fatal("the first request was refused")
	}
	if l.Allow("a", testStart.Add(testWindow-time.Nanosecond)) {
		t.Fatal("the budget reset before the window ended")
	}
	if !l.Allow("a", testStart.Add(testWindow)) {
		t.Fatal("the budget did not reset once the window ended")
	}
}

func TestFullTableRefusesNewKey(t *testing.T) {
	l := New(testOneShot, testWindow, testMaxKeys)
	if !l.Allow("a", testStart) || !l.Allow("b", testStart) {
		t.Fatal("the first two keys must be admitted")
	}
	if l.Allow("c", testStart) {
		t.Fatal("a third key was admitted into a full table")
	}
	if l.Allow("a", testStart) {
		t.Fatal("key a exceeded its budget of one")
	}
}

func TestExpiredKeyFreesRoom(t *testing.T) {
	l := New(testOneShot, testWindow, testMaxKeys)
	l.Allow("a", testStart)
	l.Allow("b", testStart.Add(testWindow/2))
	if !l.Allow("c", testStart.Add(testWindow)) {
		t.Fatal("an expired key did not free a slot for a new key")
	}
	if l.Allow("d", testStart.Add(testWindow)) {
		t.Fatal("a live key freed a slot it still holds")
	}
}

func TestExpiredKeyReturningInFullTableIsAdmitted(t *testing.T) {
	l := New(testOneShot, testWindow, testMaxKeys)
	l.Allow("a", testStart)
	l.Allow("b", testStart)
	if !l.Allow("a", testStart.Add(testWindow)) {
		t.Fatal("an expired key returning to a full table was refused")
	}
}

func TestMaxKeysIsAtLeastOne(t *testing.T) {
	l := New(testOneShot, testWindow, 0)
	if !l.Allow("a", testStart) {
		t.Fatal("the first key was refused")
	}
	if l.Allow("b", testStart) {
		t.Fatal("a second key was admitted with a table of one")
	}
}
