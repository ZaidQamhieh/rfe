// Command client pairs one device with an isolated test agent and prints its token.
//
//	client BASE FINGERPRINT CODE USER [LABEL]
//
// USER "-" pairs a plain device with POST /v1/pair; any other USER registers an
// owner account with POST /v1/register. LABEL (default "e2e-USER") is the
// device label, so every device in a run can be found by a unique label.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 5 {
		fmt.Fprintln(os.Stderr, "usage: client BASE FINGERPRINT CODE USER [LABEL]")
		os.Exit(2)
	}
	base, fp, code, user := os.Args[1], os.Args[2], os.Args[3], os.Args[4]
	label := "e2e-" + user
	if len(os.Args) > 5 {
		label = os.Args[5]
	}
	c := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	post := func(p string, body any) map[string]any {
		b, _ := json.Marshal(body)
		resp, err := c.Post(base+p, "application/json", bytes.NewReader(b))
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if resp.StatusCode >= 300 {
			fmt.Fprintf(os.Stderr, "%s -> %d %s\n", p, resp.StatusCode, raw)
			os.Exit(1)
		}
		return m
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	n := post("/v1/auth/challenge", map[string]any{})["nonce"].(string)
	msg := "rfe-device-proof-v2\n" + strings.ToLower(fp) + "\n" + n
	sig := ed25519.Sign(priv, []byte(msg))
	body := map[string]any{"pairingCode": code, "deviceLabel": label,
		"devicePublicKey": base64.StdEncoding.EncodeToString(pub), "nonce": n,
		"signature": base64.StdEncoding.EncodeToString(sig)}
	path := "/v1/pair"
	if user != "-" {
		body["username"], body["password"], path = user, "correct-horse-battery", "/v1/register"
	}
	fmt.Println(post(path, body)["deviceToken"])
}
