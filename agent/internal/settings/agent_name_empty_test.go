package settings

import "testing"

// An owner who clears the agent name must still have it empty after a restart: an empty stored value is a
// persisted value, not an unset key.
func TestLoad_KeepsExplicitlyEmptyAgentName(t *testing.T) {
	db := newDB(t)
	s, err := Load(db, false, nil, "my-pc")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAgentName(""); err != nil {
		t.Fatal(err)
	}
	s2, err := Load(db, false, nil, "my-pc")
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.AgentName(); got != "" {
		t.Fatalf("agent name after reload = %q, want the persisted empty value", got)
	}
}
