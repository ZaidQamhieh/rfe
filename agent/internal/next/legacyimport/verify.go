package legacyimport

import (
	"fmt"
	"reflect"

	nstore "github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// verify reads the freshly written database back and compares it with the
// rows that were meant to be written: SQLite's own integrity checks, the row
// count of every table, every device's token hash and account hash, and then
// every row field by field. Any difference is an error.
func verify(db *nstore.DB, want nstore.ImportData) error {
	problems, err := db.IntegrityProblems()
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("new database is not sound: %v", problems)
	}
	gotCounts, err := db.Counts()
	if err != nil {
		return err
	}
	if wantCounts := countsOf(want); !reflect.DeepEqual(gotCounts, wantCounts) {
		return fmt.Errorf("row counts differ: wrote %v, read back %v", wantCounts, gotCounts)
	}
	devices, err := db.Devices()
	if err != nil {
		return err
	}
	for i, d := range want.Devices {
		if devices[i].ID != d.ID || devices[i].TokenHash != d.TokenHash {
			return fmt.Errorf("token hash of device %s did not survive", d.ID)
		}
	}
	users, err := db.Users()
	if err != nil {
		return err
	}
	for i, u := range want.Users {
		if users[i].PasswordHash != u.PasswordHash {
			return fmt.Errorf("password hash of account %q did not survive", u.Username)
		}
	}
	links, err := db.ShareLinks()
	if err != nil {
		return err
	}
	events, err := db.ShareEvents()
	if err != nil {
		return err
	}
	uploads, err := db.Uploads()
	if err != nil {
		return err
	}
	audit, err := db.AuditEntries()
	if err != nil {
		return err
	}
	settings, err := db.Settings()
	if err != nil {
		return err
	}
	meta, err := db.MetaValues()
	if err != nil {
		return err
	}
	checks := []struct {
		table     string
		got, want any
	}{
		{"users", users, want.Users}, {"devices", devices, want.Devices},
		{"share_links", links, want.ShareLinks}, {"share_events", events, want.ShareEvents},
		{"uploads", uploads, want.Uploads}, {"audit_log", audit, want.Audit},
		{"settings", settings, want.Settings}, {"meta", meta, want.Meta},
	}
	for _, c := range checks {
		if !sameRows(c.got, c.want) {
			return fmt.Errorf("table %s differs from the rows that were written", c.table)
		}
	}
	return nil
}

// sameRows compares two row lists, treating a nil and an empty slice or map
// as equal.
func sameRows(got, want any) bool {
	g, w := reflect.ValueOf(got), reflect.ValueOf(want)
	if g.Kind() == reflect.Slice && w.Kind() == reflect.Slice && g.Len() == 0 && w.Len() == 0 {
		return true
	}
	return reflect.DeepEqual(got, want)
}
