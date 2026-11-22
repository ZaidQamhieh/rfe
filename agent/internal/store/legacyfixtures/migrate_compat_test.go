package legacyfixtures

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// tokenColumns reads every device (id, token_hash) from the file, in id order, as one comparable string.
func tokenColumns(t *testing.T, path string) string {
	t.Helper()
	db := rawDB(t, path)
	rows, err := db.Query(`SELECT id, token_hash FROM devices ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "%s=%s\n", id, hash)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// Every released schema version migrates to head with min_compatible still 0 (all steps are additive) and
// with every device token hash byte-for-byte what it was: migrations never touch token-hash rows.
func TestMigrationFromEveryVersionKeepsTokenHashesAndMinCompatible(t *testing.T) {
	for v := 0; v <= LatestVersion; v++ {
		t.Run(fmt.Sprintf("v%d", v), func(t *testing.T) {
			f := Build(t, t.TempDir(), v)
			hashesBefore := tokenColumns(t, f.DBPath)
			if strings.Count(hashesBefore, "\n") == 0 {
				t.Fatal("fixture has no devices; the test would prove nothing")
			}
			digestBefore, err := store.TokenHashDigest(f.DataDir)
			if err != nil {
				t.Fatalf("digest before: %v", err)
			}

			st, err := store.Open(f.DataDir)
			if err != nil {
				t.Fatalf("store.Open: %v", err)
			}
			uv, minCompat, err := st.SchemaState()
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			if uv != store.SchemaVersion() || minCompat != 0 {
				t.Fatalf("user_version=%d min_compatible=%d, want %d and 0", uv, minCompat, store.SchemaVersion())
			}
			if after := tokenColumns(t, f.DBPath); after != hashesBefore {
				t.Fatalf("device token hashes changed by migration:\n before:\n%s after:\n%s", hashesBefore, after)
			}
			digestAfter, err := store.TokenHashDigest(f.DataDir)
			if err != nil || digestAfter != digestBefore {
				t.Fatalf("token-hash digest %s -> %s (%v)", digestBefore, digestAfter, err)
			}
		})
	}
}
