// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleustest"

	"github.com/jcsvwinston/orbit"
)

// signInTo signs in to any booted application and returns a client holding
// the admin session.
func signInTo(t *testing.T, srv *nucleustest.Server, username, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.PostForm(srv.URL("/admin/login"),
		url.Values{"username": {username}, "password": {password}})
	if err != nil {
		t.Fatalf("sign in as %q: %v", username, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		t.Fatalf("sign in as %q answered %d: %s", username, resp.StatusCode, truncate(string(body)))
	}
	return client
}

// mapText renders a decoded payload back to text so a probe can ask whether a
// value appears anywhere in it.
func mapText(payload map[string]any) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(raw)
}

func entryText(entry map[string]any) string { return mapText(entry) }

// configHasKey reports whether the mount surface — orbit.Config, the only
// thing an application configures — carries a knob by that yaml/koanf name.
// It is a measurement of the CONTRACT, not a grep: the freeze test makes this
// struct the whole of what an application can set.
func configHasKey(key string) bool {
	typ := reflect.TypeOf(orbit.Config{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		for _, tag := range []string{"yaml", "koanf"} {
			if name := strings.Split(f.Tag.Get(tag), ",")[0]; name == key {
				return true
			}
		}
		if strings.EqualFold(f.Name, key) {
			return true
		}
	}
	return false
}

// configKeysMatching returns the mount keys whose name contains any of the
// given fragments — the probe behind every "can an application brand / theme
// / translate this panel" question.
func configKeysMatching(fragments ...string) []string {
	typ := reflect.TypeOf(orbit.Config{})
	hits := []string{}
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("koanf"), ",")[0]
		if name == "" {
			name = strings.ToLower(typ.Field(i).Name)
		}
		for _, fragment := range fragments {
			if strings.Contains(name, fragment) {
				hits = append(hits, name)
				break
			}
		}
	}
	return hits
}

// topLevelKeys lists the keys of a payload, sorted, so a probe's log says what
// the panel actually answered instead of the first 400 bytes of it.
func topLevelKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for k := range payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sessionHandleOf derives the panel's opaque row id for an operator's session
// from the cookie that operator holds: the panel keys rows by a truncated
// SHA-256 of the session token.
func sessionHandleOf(t *testing.T, e *env, op *operator) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.server().URL("/admin"), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	for _, cookie := range op.client.Jar.Cookies(req.URL) {
		if cookie.Name != "session" {
			continue
		}
		sum := sha256.Sum256([]byte(cookie.Value))
		return hex.EncodeToString(sum[:16])
	}
	return ""
}

// requestAs issues one request against an application other than the shared
// one, for the probes that need their own posture.
func requestAs(t *testing.T, client *http.Client, srv *nucleustest.Server, method, path string, payload any) response {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode payload: %v", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, srv.URL(path), body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response{code: resp.StatusCode, ctype: resp.Header.Get("Content-Type"), body: raw}
}
