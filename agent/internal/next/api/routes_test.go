package api_test

import (
	"bufio"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
)

// specPath is protocol/openapi.yaml seen from this package's directory.
const specPath = "../../../../protocol/openapi.yaml"

const (
	specPathsKey  = "paths:"
	testDeviceID  = "dev-1"
	fileCapCount  = 6
	fileGrantMask = 1<<fileCapCount - 1
)

var (
	paramName  = regexp.MustCompile(`\{[^}]*\}`)
	pathLine   = regexp.MustCompile(`^  (/[^\s:]*):\s*$`)
	methodLine = regexp.MustCompile(`^    (get|put|post|patch|delete):\s*$`)
)

// shape drops path-parameter names so /share/{token} and /share/{tokenHash}
// compare equal, the same rule the old drift test uses
// (agent/internal/server/openapi_drift_test.go:20).
func shape(op string) string { return paramName.ReplaceAllString(op, "{}") }

// specOperations reads "METHOD /path" for every operation under `paths:` in
// the OpenAPI file, paths relative to the server URL (which ends in /v1).
// gopkg.in/yaml is not in agent/go.mod, so this scans lines like the old test
// (openapi_drift_test.go:25-62).
func specOperations(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open(specPath)
	if err != nil {
		t.Fatalf("open spec: %v", err)
	}
	defer f.Close()
	ops := map[string]bool{}
	inPaths := false
	current := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !inPaths {
			inPaths = line == specPathsKey
			continue
		}
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#") {
			break
		}
		if m := pathLine.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if m := methodLine.FindStringSubmatch(line); m != nil && current != "" {
			ops[shape(strings.ToUpper(m[1])+" "+current)] = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if len(ops) == 0 {
		t.Fatal("found no operations in the spec; the parser needs updating")
	}
	return ops
}

func TestRouteTableEqualsOpenAPIOperations(t *testing.T) {
	spec := specOperations(t)
	table := map[string]bool{}
	for _, r := range api.Routes {
		key := shape(r.Method + " " + r.Pattern)
		if table[key] {
			t.Errorf("route %s is listed twice", key)
		}
		table[key] = true
	}
	var notInSpec, notInTable []string
	for k := range table {
		if !spec[k] {
			notInSpec = append(notInSpec, k)
		}
	}
	for k := range spec {
		if !table[k] {
			notInTable = append(notInTable, k)
		}
	}
	sort.Strings(notInSpec)
	sort.Strings(notInTable)
	if len(notInSpec) > 0 {
		t.Errorf("routes in the table but not in protocol/openapi.yaml: %v", notInSpec)
	}
	if len(notInTable) > 0 {
		t.Errorf("operations in protocol/openapi.yaml with no route in the table: %v", notInTable)
	}
}

// Every route registered by the old agent (agent/internal/server/server.go:
// 144-247 and the groups at :78-129) must be in the table with the tier the old
// middleware chain gave it. Written out by hand, independent of routes.go.
var oldTiers = map[string]api.Tier{
	// registerUnauthRoutes, server.go:145-154
	"GET /health": api.TierPublic, "POST /auth/challenge": api.TierPublic, "POST /auth/logout": api.TierPublic,
	"POST /pair": api.TierPublic, "POST /pair/request": api.TierPublic, "POST /pair/request/{id}/reveal": api.TierPublic,
	"GET /pair/request/{id}": api.TierPublic, "POST /register": api.TierPublic, "POST /login": api.TierPublic,
	"GET /share/{token}": api.TierPublic,
	"GET /events":        api.TierDevice, // push channel (rfe-ahy), behind the browse grant
	"GET /accounts/key":  api.TierPublic, "POST /accounts/assertion": api.TierAdmin,
	"GET /settings/account-server": api.TierAdmin, "PUT /settings/account-server": api.TierAdmin, "DELETE /settings/account-server": api.TierAdmin,
	// authenticated group, server.go:78-94
	"GET /status": api.TierDevice, "GET /transfers/list": api.TierDevice, "DELETE /transfers/{id}": api.TierDevice,
	"GET /apps": api.TierDevice, "POST /apps/{id}/launch": api.TierDevice,
	// adminOnly group, server.go:100-109
	"GET /metrics": api.TierAdmin, "GET /system/backups": api.TierAdmin, "GET /users": api.TierAdmin, "DELETE /users/{username}": api.TierAdmin,
	"GET /logs": api.TierAdmin, "GET /audit": api.TierAdmin, "POST /agent/restart": api.TierAdmin,
	// registerSettingsAndDeviceRoutes, server.go:161-181; admin checks live in the
	// handlers: settings_handlers.go:166,211,225,382; pair.go:180; pair_request.go:307
	"GET /settings": api.TierDevice, "PATCH /settings": api.TierAdmin,
	"GET /settings/bandwidth": api.TierAdmin, "PUT /settings/bandwidth": api.TierAdmin,
	"GET /devices": api.TierDevice, "PATCH /devices/{id}": api.TierAdmin, "DELETE /devices/{id}": api.TierDevice,
	"POST /wol": api.TierDevice, "POST /pairing/generate": api.TierAdmin,
	"GET /pair/requests": api.TierAdmin, "POST /pair/requests/{id}/approve": api.TierAdmin, "POST /pair/requests/{id}/reject": api.TierAdmin,
	// registerShareRoutes, server.go:188-190
	"POST /share/mint": api.TierDevice, "DELETE /share/{tokenHash}": api.TierDevice, "GET /share": api.TierDevice,
	// server.go:121-123, registerUpdateRoutes :195-196
	"GET /system/drives": api.TierDevice, "GET /search": api.TierDevice, "GET /thumb": api.TierDevice,
	"GET /app/latest": api.TierDevice, "GET /app/download": api.TierDevice,
	// registerFsRoutes, server.go:201-216
	"GET /fs": api.TierDevice, "DELETE /fs": api.TierDevice, "POST /fs/folder": api.TierDevice, "POST /fs/file": api.TierDevice,
	"PATCH /fs/rename": api.TierDevice, "POST /fs/copy": api.TierDevice, "POST /fs/move": api.TierDevice,
	"POST /fs/compress": api.TierDevice, "POST /fs/extract": api.TierDevice, "GET /fs/meta": api.TierDevice,
	"GET /fs/media-info": api.TierDevice, "GET /fs/checksum": api.TierDevice, "POST /fs/chmod": api.TierDevice,
	"GET /fs/archive": api.TierDevice, "POST /fs/checksums": api.TierDevice, "GET /fs/recent": api.TierDevice, "GET /fs/usage": api.TierDevice,
	// registerTrashRoutes :221-223, registerContentRoutes :229-231, registerTransferRoutes :239-245
	"GET /trash": api.TierDevice, "POST /trash/restore": api.TierDevice, "DELETE /trash": api.TierDevice,
	"GET /content": api.TierDevice, "PUT /content": api.TierDevice, "GET /stream": api.TierDevice,
	"GET /transfers/{id}": api.TierDevice, "POST /transfers": api.TierDevice,
	"PUT /transfers/{id}/chunks/{n}": api.TierDevice, "POST /transfers/{id}/complete": api.TierDevice,
}

func routeKey(r api.Route) string { return r.Method + " " + r.Pattern }

func TestRouteTableHasTheOldAgentsRoutesAndTiers(t *testing.T) {
	got := map[string]api.Tier{}
	for _, r := range api.Routes {
		got[routeKey(r)] = r.Tier
	}
	for k, want := range oldTiers {
		tier, ok := got[k]
		if !ok {
			t.Errorf("route %q of the old agent is missing from the table", k)
			continue
		}
		if tier != want {
			t.Errorf("route %q tier=%v, old agent=%v", k, tier, want)
		}
	}
	for k := range got {
		if _, ok := oldTiers[k]; !ok {
			t.Errorf("route %q is in the table but the old agent does not serve it", k)
		}
	}
}

func TestTierAndActionAreConsistent(t *testing.T) {
	for _, r := range api.Routes {
		switch r.Tier {
		case api.TierPublic:
			if r.Action != authz.ActionNone {
				t.Errorf("%s: a public route has no principal, so it cannot require %v", routeKey(r), r.Action)
			}
		case api.TierAdmin:
			if r.Action != authz.ActionAdmin {
				t.Errorf("%s: admin tier must carry ActionAdmin, has %v", routeKey(r), r.Action)
			}
		case api.TierDevice:
			if r.Action == authz.ActionAdmin {
				t.Errorf("%s: ActionAdmin belongs on the admin tier", routeKey(r))
			}
		default:
			t.Errorf("%s: unknown tier %v", routeKey(r), r.Tier)
		}
		if r.Method == "" || !strings.HasPrefix(r.Pattern, "/") {
			t.Errorf("malformed route %+v", r)
		}
		if r.Method != strings.ToUpper(r.Method) {
			t.Errorf("%s: method must be upper case", routeKey(r))
		}
	}
}

// oldFileCaps is the capability list the old agent's requireFileCapabilities
// puts in front of each route (agent/internal/server/server.go line cited).
var oldFileCaps = map[string][]authz.Capability{
	"POST /share/mint":               {authz.CapShare, authz.CapBrowse, authz.CapDownload}, // :188
	"GET /system/drives":             {authz.CapBrowse},                                    // :121
	"GET /events":                    {authz.CapBrowse},                                    // push channel (rfe-ahy)
	"GET /search":                    {authz.CapBrowse},                                    // :122
	"GET /thumb":                     {authz.CapDownload},                                  // :123
	"GET /fs":                        {authz.CapBrowse},                                    // :201
	"DELETE /fs":                     {authz.CapDelete},                                    // :202
	"POST /fs/folder":                {authz.CapModify},                                    // :203
	"POST /fs/file":                  {authz.CapModify},                                    // :204
	"PATCH /fs/rename":               {authz.CapModify},                                    // :205
	"POST /fs/copy":                  {authz.CapModify},                                    // :206
	"POST /fs/move":                  {authz.CapModify, authz.CapDelete},                   // :207
	"POST /fs/compress":              {authz.CapModify},                                    // :208
	"POST /fs/extract":               {authz.CapModify},                                    // :209
	"GET /fs/meta":                   {authz.CapBrowse},                                    // :210
	"GET /fs/media-info":             {authz.CapBrowse},                                    // :211
	"GET /fs/checksum":               {authz.CapDownload},                                  // :212
	"POST /fs/chmod":                 {authz.CapModify},                                    // :213
	"GET /fs/archive":                {authz.CapBrowse},                                    // :214
	"POST /fs/checksums":             {authz.CapDownload},                                  // :215
	"GET /fs/recent":                 {authz.CapBrowse},                                    // :216
	"GET /fs/usage":                  {authz.CapBrowse},                                    // new in 1.1
	"GET /trash":                     {authz.CapBrowse},                                    // :221
	"POST /trash/restore":            {authz.CapModify},                                    // :222
	"DELETE /trash":                  {authz.CapDelete},                                    // :223
	"GET /content":                   {authz.CapDownload},                                  // :229
	"PUT /content":                   {authz.CapModify},                                    // :231
	"GET /stream":                    {authz.CapDownload},                                  // :233
	"POST /transfers":                {authz.CapUpload},                                    // :241
	"PUT /transfers/{id}/chunks/{n}": {authz.CapUpload},                                    // :241
	"POST /transfers/{id}/complete":  {authz.CapUpload},                                    // :241
}

// Routes that the old agent guards with requireWritable (server.go:242): the
// resumable-upload writers. Everything else that writes reaches READ_ONLY inside
// fsops, which the route table models as Writes().
var oldWritableGuarded = map[string]bool{
	"POST /transfers": true, "PUT /transfers/{id}/chunks/{n}": true, "POST /transfers/{id}/complete": true,
}

func grants(mask int) authz.Grants {
	return authz.Grants{
		Browse: mask&(1<<0) != 0, Download: mask&(1<<1) != 0, Upload: mask&(1<<2) != 0,
		Modify: mask&(1<<3) != 0, Delete: mask&(1<<4) != 0, Share: mask&(1<<5) != 0,
	}
}

func hasCap(g authz.Grants, c authz.Capability) bool {
	switch c {
	case authz.CapBrowse:
		return g.Browse
	case authz.CapDownload:
		return g.Download
	case authz.CapUpload:
		return g.Upload
	case authz.CapModify:
		return g.Modify
	case authz.CapDelete:
		return g.Delete
	case authz.CapShare:
		return g.Share
	}
	return false
}

// For every route and every grant combination the new table must allow exactly
// what the old agent's middleware allowed (auth.go:175-191).
func TestEveryRouteAgreesWithTheOldAgentForAllGrantCombinations(t *testing.T) {
	for _, r := range api.Routes {
		if r.Tier != api.TierDevice {
			continue
		}
		required, listed := oldFileCaps[routeKey(r)]
		for mask := 0; mask <= fileGrantMask; mask++ {
			for _, admin := range []bool{false, true} {
				g := grants(mask)
				p := authz.Principal{DeviceID: testDeviceID, Admin: admin, Grants: g}
				d := authz.Decide(p, r.Action, authz.Target{DeviceID: testDeviceID}, authz.Policy{})
				wantAllowed := true
				var wantMissing authz.Capability
				if listed && !admin {
					for _, c := range required {
						if !hasCap(g, c) {
							wantAllowed, wantMissing = false, c
							break
						}
					}
				}
				if r.Action == authz.ActionViewApps || r.Action == authz.ActionLaunchApps || r.Action == authz.ActionWakeOnLAN {
					continue // covered by the authz tests: they follow other rules
				}
				if d.Allowed != wantAllowed || d.Missing != wantMissing {
					t.Fatalf("%s admin=%v mask=%06b: allowed=%v missing=%q, old agent allowed=%v missing=%q",
						routeKey(r), admin, mask, d.Allowed, d.Missing, wantAllowed, wantMissing)
				}
			}
		}
	}
}

func TestWritesMatchTheOldReadOnlyGuard(t *testing.T) {
	for _, r := range api.Routes {
		if oldWritableGuarded[routeKey(r)] && !r.Writes() {
			t.Errorf("%s was behind requireWritable (server.go:242); the table must mark it as writing", routeKey(r))
		}
	}
	// Reads must never be marked as writes: a read-only agent still browses.
	for _, r := range api.Routes {
		if r.Method == http.MethodGet && r.Writes() {
			t.Errorf("%s is a GET but marked as a write", routeKey(r))
		}
	}
}

func TestAppAndDeviceSpecialCases(t *testing.T) {
	want := map[string]authz.Action{
		"GET /apps":              authz.ActionViewApps,    // apps_handlers.go:55
		"POST /apps/{id}/launch": authz.ActionLaunchApps,  // apps_handlers.go:90
		"POST /wol":              authz.ActionWakeOnLAN,   // wol_handler.go:30
		"DELETE /devices/{id}":   authz.ActionSelfOrAdmin, // settings_handlers.go:329,347
		"POST /share/mint":       authz.ActionMintShare,   // server.go:188
		"POST /fs/move":          authz.ActionMove,        // server.go:207
	}
	got := map[string]authz.Action{}
	for _, r := range api.Routes {
		got[routeKey(r)] = r.Action
	}
	for k, a := range want {
		if got[k] != a {
			t.Errorf("%s: action %v, want %v", k, got[k], a)
		}
	}
}
