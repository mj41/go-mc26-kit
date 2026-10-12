package microsoft

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// fakeHost is the only host the tests' transport serves: a request to any other — a real Microsoft or Xbox
// endpoint, an endpoint the options forgot to point at the fake — fails the request.
const fakeHost = "ms.test"

var fakeEndpoints = Endpoints{
	LiveDeviceCode:   "http://" + fakeHost + "/live/devicecode",
	LiveToken:        "http://" + fakeHost + "/live/token",
	XboxUser:         "http://" + fakeHost + "/xbox/user",
	XboxDevice:       "http://" + fakeHost + "/xbox/device",
	XboxTitle:        "http://" + fakeHost + "/xbox/title",
	XboxXSTS:         "http://" + fakeHost + "/xbox/xsts",
	MinecraftLogin:   "http://" + fakeHost + "/mc/login",
	MinecraftProfile: "http://" + fakeHost + "/mc/profile",
}

// fakeServices plays Microsoft's, Xbox Live's and Minecraft's login services for a test, with made-up tokens
// for a made-up player, Steve: each step checks it got the token of the step before, and an Xbox request's
// signature is verified against the proof key it carries.
type fakeServices struct {
	t *testing.T

	mu       sync.Mutex
	polls    int            // device-code polls so far
	calls    map[string]int // requests by path
	refresh  string         // the refresh token the token service takes now
	xstsXErr int64          // set: XSTS refuses with this Xbox error
}

func newFake(t *testing.T) *fakeServices {
	return &fakeServices{t: t, calls: map[string]int{}, refresh: "fake-refresh-1"}
}

func (f *fakeServices) client() *http.Client { return &http.Client{Transport: f} }

func (f *fakeServices) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != fakeHost {
		return nil, fmt.Errorf("the test reached for %s: only the fake %s is served", r.URL.Host, fakeHost)
	}
	rec := httptest.NewRecorder()
	f.serve(rec, r)
	return rec.Result(), nil
}

func (f *fakeServices) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[path]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeServices) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[r.URL.Path]++
	var body []byte
	if r.Body != nil { // a client's GET has none (the transport is handed the client's request)
		body, _ = io.ReadAll(r.Body)
	}
	var in map[string]any
	if strings.HasPrefix(r.URL.Path, "/xbox/") || r.URL.Path == "/mc/login" {
		if err := json.Unmarshal(body, &in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad json"})
			return
		}
	}
	props, _ := in["Properties"].(map[string]any)
	switch r.URL.Path {
	case "/live/devicecode":
		form, _ := url.ParseQuery(string(body))
		if form.Get("response_type") != "device_code" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"user_code": "FAKE-CODE", "device_code": "fake-device-code",
			"verification_uri": "https://example.invalid/link", "expires_in": 900, "interval": 5})
	case "/live/token":
		form, _ := url.ParseQuery(string(body))
		switch form.Get("grant_type") {
		case "urn:ietf:params:oauth:grant-type:device_code":
			f.polls++
			switch f.polls {
			case 1: // the player has not entered the code yet
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "authorization_pending"})
			case 2:
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "slow_down"})
			default:
				writeJSON(w, http.StatusOK, map[string]any{"access_token": "fake-msa-access-1", "refresh_token": "fake-refresh-1", "expires_in": 3600})
			}
		case "refresh_token":
			if form.Get("refresh_token") != f.refresh {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "the refresh token is unknown"})
				return
			}
			f.refresh = "fake-refresh-2"
			writeJSON(w, http.StatusOK, map[string]any{"access_token": "fake-msa-access-2", "refresh_token": "fake-refresh-2", "expires_in": 3600})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported_grant_type"})
		}
	case "/xbox/user":
		if t, _ := props["RpsTicket"].(string); !strings.HasPrefix(t, "t=fake-msa-access-") {
			writeJSON(w, http.StatusUnauthorized, map[string]any{})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"Token": "fake-user"})
	case "/xbox/device", "/xbox/title", "/xbox/xsts":
		if err := verifySignature(r, body, props); err != nil {
			f.t.Errorf("%s: the request's signature: %v", r.URL.Path, err)
			writeJSON(w, http.StatusUnauthorized, map[string]any{})
			return
		}
		switch r.URL.Path {
		case "/xbox/device":
			writeJSON(w, http.StatusOK, map[string]any{"Token": "fake-device"})
		case "/xbox/title":
			if props["DeviceToken"] != "fake-device" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"Token": "fake-title"})
		case "/xbox/xsts":
			users, _ := props["UserTokens"].([]any)
			if len(users) != 1 || users[0] != "fake-user" || props["DeviceToken"] != "fake-device" || props["TitleToken"] != "fake-title" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{})
				return
			}
			if f.xstsXErr != 0 {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"XErr": f.xstsXErr, "Message": ""})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"Token": "fake-xsts", "DisplayClaims": map[string]any{"xui": []any{map[string]any{"uhs": "fake-uhs"}}}})
		}
	case "/mc/login":
		if in["identityToken"] != "XBL3.0 x=fake-uhs;fake-xsts" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized", "path": r.URL.Path})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "fake-mc-token", "expires_in": 86400, "token_type": "Bearer"})
	case "/mc/profile":
		if r.Header.Get("Authorization") != "Bearer fake-mc-token" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": "00000000000040008000000000000001", "name": "Steve"})
	default:
		http.NotFound(w, r)
	}
}

// verifySignature checks an Xbox request's Signature header as the service does: version 1, a Windows
// timestamp, and an ES256 signature over (version, timestamp, method, path, authorization, body) by the
// ProofKey the body carries.
func verifySignature(r *http.Request, body []byte, props map[string]any) error {
	key, _ := props["ProofKey"].(map[string]any)
	x, errX := base64.RawURLEncoding.DecodeString(fmt.Sprint(key["x"]))
	y, errY := base64.RawURLEncoding.DecodeString(fmt.Sprint(key["y"]))
	if errX != nil || errY != nil || len(x) != 32 || len(y) != 32 {
		return errors.New("no P-256 proof key in the body")
	}
	sig, err := base64.StdEncoding.DecodeString(r.Header.Get("Signature"))
	if err != nil || len(sig) != 4+8+64 || binary.BigEndian.Uint32(sig) != 1 {
		return fmt.Errorf("not a version-1 signature (%d bytes)", len(sig))
	}
	var msg bytes.Buffer
	msg.Write(sig[:4])
	msg.WriteByte(0)
	msg.Write(sig[4:12])
	msg.WriteByte(0)
	for _, part := range []string{r.Method, r.URL.Path, r.Header.Get("Authorization"), string(body)} {
		msg.WriteString(part)
		msg.WriteByte(0)
	}
	hash := sha256.Sum256(msg.Bytes())
	pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
	if !ecdsa.Verify(pub, hash[:], new(big.Int).SetBytes(sig[12:44]), new(big.Int).SetBytes(sig[44:76])) {
		return errors.New("it does not verify")
	}
	return nil
}

func (f *fakeServices) options(cacheDir string) (Options, *[]DeviceCode) {
	var codes []DeviceCode
	return Options{CacheDir: cacheDir, CacheKey: "steve", HTTPClient: f.client(), Endpoints: fakeEndpoints,
		OnDeviceCode: func(c DeviceCode) { codes = append(codes, c) }}, &codes
}

// cacheFile is the one cache file in dir; it fails the test unless its mode is 0600 (a refresh token in it).
func cacheFile(t *testing.T, dir string) cachedLiveState {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 1 {
		t.Fatalf("%d cache files in %s, want 1", len(files), dir)
	}
	fi, err := os.Stat(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Errorf("the cache file's mode is %o, want 600: it holds the refresh token", mode)
	}
	data, _ := os.ReadFile(files[0])
	var st cachedLiveState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

// The first login, interactive: the device code shown, its approval polled for (pending, then slow down,
// then the token), Xbox Live's four steps, Minecraft's login and profile; the tokens cached 0600.
func TestDeviceCodeLogin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake, dir := newFake(t), t.TempDir()
		opts, codes := fake.options(dir)
		a, err := AuthenticateDeviceCode(t.Context(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if a.AccessToken() != "fake-mc-token" || a.Profile.Name != "Steve" {
			t.Errorf("the login: token %q, profile %+v, want the fake's Steve", a.AccessToken(), a.Profile)
		}
		if len(*codes) != 1 || (*codes)[0].UserCode != "FAKE-CODE" || !strings.Contains((*codes)[0].Message, "FAKE-CODE") {
			t.Errorf("the device code shown: %+v", *codes)
		}
		if fake.polls != 3 {
			t.Errorf("%d polls for the approval, want 3 (pending, slow down, the token)", fake.polls)
		}
		st := cacheFile(t, dir)
		if st.Token.RefreshToken != "fake-refresh-1" || st.Profile == nil || st.Profile.Name != "Steve" {
			t.Errorf("the cache: refresh %q, profile %+v", st.Token.RefreshToken, st.Profile)
		}
	})
}

// A later login from the cache: the cached Microsoft token while it is valid (no device code, no token
// request); once it has expired, the refresh token is used and the new tokens cached, still 0600.
func TestCachedLogin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake, dir := newFake(t), t.TempDir()
		opts, _ := fake.options(dir)
		if _, err := AuthenticateDeviceCode(t.Context(), opts); err != nil {
			t.Fatal(err)
		}
		before := fake.count("/live/token")

		if a, err := AuthenticateCached(t.Context(), opts); err != nil || a.Profile.Name != "Steve" {
			t.Fatalf("from the cache: %+v, %v", a, err)
		}
		if n := fake.count("/live/devicecode"); n != 1 {
			t.Errorf("%d device-code requests, want only the first login's", n)
		}
		if n := fake.count("/live/token"); n != before {
			t.Errorf("the cached valid token was not used: %d token requests after it", n-before)
		}

		time.Sleep(2 * time.Hour) // the Microsoft token (an hour) expired
		if _, err := AuthenticateCached(t.Context(), opts); err != nil {
			t.Fatalf("after the token expired: %v", err)
		}
		if st := cacheFile(t, dir); st.Token.RefreshToken != "fake-refresh-2" || st.Token.AccessToken != "fake-msa-access-2" {
			t.Errorf("after the refresh the cache holds %q / %q, want the new tokens", st.Token.AccessToken, st.Token.RefreshToken)
		}
	})
}

// A refresh token the service no longer takes: the cache is cleared and the login asks for the player
// (ErrInteractiveLoginRequired), not a loop of refusals.
func TestRefreshRejected(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake, dir := newFake(t), t.TempDir()
		opts, _ := fake.options(dir)
		if _, err := AuthenticateDeviceCode(t.Context(), opts); err != nil {
			t.Fatal(err)
		}
		fake.mu.Lock()
		fake.refresh = "revoked" // the player signed out everywhere
		fake.mu.Unlock()
		time.Sleep(2 * time.Hour)
		if _, err := AuthenticateCached(t.Context(), opts); !errors.Is(err, ErrInteractiveLoginRequired) {
			t.Fatalf("with the refresh token refused: %v, want ErrInteractiveLoginRequired", err)
		}
		if files, _ := filepath.Glob(filepath.Join(dir, "*.json")); len(files) != 0 {
			t.Errorf("the refused cache was kept: %v", files)
		}
	})
}

// An Xbox Live refusal (XErr) is told in words the player can act on.
func TestXboxRefusal(t *testing.T) {
	for xerr, want := range map[int64]string{
		2148916233: "does not have an Xbox profile",
		2148916238: "must be added to a family",
	} {
		t.Run(fmt.Sprint(xerr), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fake := newFake(t)
				fake.xstsXErr = xerr
				opts, _ := fake.options(t.TempDir())
				_, err := AuthenticateDeviceCode(context.Background(), opts)
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("XSTS refusing with %d: %v, want %q", xerr, err, want)
				}
			})
		})
	}
}

// The cache file is 0600 after a save whatever was there before: an older cache file readable by others, or
// a temporary file a crash left with that mode.
func TestCacheFileMode(t *testing.T) {
	dir := t.TempDir()
	c, err := newTokenCache(dir, "steve", DefaultClientID)
	if err != nil {
		t.Fatal(err)
	}
	for _, left := range []string{c.path, c.path + ".tmp"} {
		if err := os.WriteFile(left, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(left, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.save(cachedLiveState{ClientID: DefaultClientID, ObtainedAt: time.Now(), Token: liveTokenResponse{AccessToken: "fake-a", RefreshToken: "fake-r", ExpiresIn: 3600}}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(c.path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Errorf("the cache file's mode after a save is %o, want 600", mode)
	}
}
