// Package yggdrasil is a client of Mojang's legacy authentication server
// (Yggdrasil: authenticate, refresh, validate, sign out). Mojang accounts were
// moved to Microsoft accounts, so a program logging in to Minecraft uses
// package microsoft; this one remains for Yggdrasil-compatible third-party
// servers (set AuthURL).
package yggdrasil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type Error struct {
	Err    string `json:"error"`
	ErrMsg string `json:"errorMessage"`
	Cause  string `json:"cause"`
}

func (e Error) Error() string {
	return e.Err + ": " + e.ErrMsg + ", " + e.Cause
}

// AuthURL is the authentication server's base URL.
var AuthURL = "https://authserver.mojang.com"

var client = http.DefaultClient

func post(endpoint string, payload any, resp any) error {
	rowResp, err := rawPost(endpoint, payload)
	if err != nil {
		return fmt.Errorf("request fail: %v", err)
	}
	defer rowResp.Body.Close()

	err = json.NewDecoder(rowResp.Body).Decode(resp)
	if err != nil {
		return fmt.Errorf("parse resp fail: %v", err)
	}

	return nil
}

func rawPost(endpoint string, payload any) (*http.Response, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload fail: %v", err)
	}

	PostRequest, err := http.NewRequest(
		http.MethodPost,
		AuthURL+endpoint,
		bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("make request error: %v", err)
	}

	PostRequest.Header.Set("User-agent", "go-mc")
	PostRequest.Header.Set("Connection", "keep-alive")
	PostRequest.Header.Set("Content-Type", "application/json")

	// Do
	return client.Do(PostRequest)
}
