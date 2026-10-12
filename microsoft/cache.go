package microsoft

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const liveTokenValiditySkew = time.Minute

type tokenCache struct {
	path string
}

type cachedLiveState struct {
	Version    int               `json:"version"`
	ClientID   string            `json:"client_id"`
	ObtainedAt time.Time         `json:"obtained_at"`
	Token      liveTokenResponse `json:"token"`
	Profile    *Profile          `json:"profile,omitempty"`
}

type liveTokenError struct {
	Code        string
	Description string
}

func (e *liveTokenError) Error() string {
	if e.Description == "" {
		return fmt.Sprintf("Microsoft token error: %s", e.Code)
	}
	return fmt.Sprintf("Microsoft token error: %s (%s)", e.Code, e.Description)
}

func newTokenCache(cacheDir, cacheKey, clientID string) (*tokenCache, error) {
	if cacheDir == "" {
		userCacheDir, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("resolve user cache dir: %w", err)
		}
		cacheDir = filepath.Join(userCacheDir, "go-mc", "microsoft")
	}
	if cacheKey == "" {
		cacheKey = "default"
	}
	fileName := sanitizeCacheKey(clientID + "-" + cacheKey)
	if fileName == "" {
		fileName = "default"
	}
	return &tokenCache{path: filepath.Join(cacheDir, fileName+".json")}, nil
}

func (c *tokenCache) load() (*cachedLiveState, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var state cachedLiveState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (c *tokenCache) save(state cachedLiveState) error {
	state.Version = 1
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	// a new file for every save, created 0600 (os.CreateTemp): os.WriteFile on a fixed name keeps the mode of
	// a file left there before (a crash's 0644 .tmp), and the refresh token would be readable by others
	tmp, err := os.CreateTemp(filepath.Dir(c.path), filepath.Base(c.path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), c.path)
}

func (c *tokenCache) clear() error {
	err := os.Remove(c.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func liveTokenStillValid(state *cachedLiveState) bool {
	if state == nil || state.Token.AccessToken == "" || state.ObtainedAt.IsZero() {
		return false
	}
	expiresAt := state.ObtainedAt.Add(time.Duration(state.Token.ExpiresIn) * time.Second)
	return time.Now().Add(liveTokenValiditySkew).Before(expiresAt)
}

func sanitizeCacheKey(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}
