package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/net"
	"github.com/mj41/go-mc26/net/CFB8"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/login"
	"github.com/mj41/go-mc26/yggdrasil/user"
)

// Deprecated: use the same types of github.com/mj41/go-mc26/yggdrasil/user.
type (
	Texture   = user.Texture
	Property  = user.Property
	PublicKey = user.PublicKey
)

const verifyTokenLen = 16

// Session is the session server a login is verified with: Mojang's unless URL says otherwise.
type Session struct {
	URL    string       // its base URL; empty: https://sessionserver.mojang.com
	Client *http.Client // nil: http.DefaultClient
}

// ErrUnverified is a session server's answer that the player did not join this server: a client that did
// not authenticate, or authenticated with another digest. A vanilla server disconnects it with
// multiplayer.disconnect.unverified_username ("Failed to verify username!").
var ErrUnverified = errors.New("auth: the session server has no record of the player joining")

// Encrypt a connection, with authentication at Mojang's session server.
func Encrypt(conn *net.Conn, name string, serverKey *rsa.PrivateKey) (*Resp, error) {
	return EncryptSession(conn, name, serverKey, Session{})
}

// EncryptSession is Encrypt with the player verified at session.
func EncryptSession(conn *net.Conn, name string, serverKey *rsa.PrivateKey, session Session) (*Resp, error) {
	publicKey, err := x509.MarshalPKIXPublicKey(&serverKey.PublicKey)
	if err != nil {
		return nil, err
	}

	verifyToken := make([]byte, verifyTokenLen)
	_, err = rand.Read(verifyToken)
	if err != nil {
		return nil, err
	}

	// encryption request
	err = encryptionRequest(conn, publicKey, verifyToken)
	if err != nil {
		return nil, err
	}

	// encryption response
	SharedSecret, err := encryptionResponse(conn, serverKey, verifyToken)
	if err != nil {
		return nil, err
	}

	// encryption the connection
	block, err := aes.NewCipher(SharedSecret)
	if err != nil {
		return nil, errors.New("load aes encryption key fail")
	}

	conn.SetCipher( // 启用加密
		CFB8.NewCFB8Encrypt(block, SharedSecret),
		CFB8.NewCFB8Decrypt(block, SharedSecret),
	)
	hash := authDigest("", SharedSecret, publicKey)
	return authentication(session, name, hash)
}

// encryptionRequest sends the login-state clientbound hello: an empty server
// id, the server's public key and the challenge the client must encrypt.
// ShouldAuthenticate asks the client to join the session server.
func encryptionRequest(conn *net.Conn, publicKey, verifyToken []byte) error {
	hello := login.ClientboundHello{
		ServerID:           "",
		PublicKey:          publicKey,
		Challenge:          verifyToken,
		ShouldAuthenticate: true,
	}
	return conn.WritePacket(pk.Marshal(hello.PacketID(), hello))
}

func encryptionResponse(conn *net.Conn, serverKey *rsa.PrivateKey, verifyToken []byte) ([]byte, error) {
	var p pk.Packet
	err := conn.ReadPacket(&p)
	if err != nil {
		return nil, err
	}
	var key login.Key
	if packetid.ServerboundPacketID(p.ID) != key.PacketID() {
		return nil, fmt.Errorf("0x%02X is not Encryption Response", p.ID)
	}
	if err = p.Scan(&key); err != nil {
		return nil, err
	}

	// confirm to verify token
	decryptedVerifyToken, err := rsa.DecryptPKCS1v15(rand.Reader, serverKey, key.EncryptedChallenge)
	if err != nil {
		return nil, err
	} else if !bytes.Equal(verifyToken, decryptedVerifyToken) {
		return nil, errors.New("verifyToken not match")
	}

	// get sharedSecret
	sharedSecret, err := rsa.DecryptPKCS1v15(rand.Reader, serverKey, key.Keybytes)
	if err != nil {
		return nil, err
	}

	return sharedSecret, nil
}

// authentication asks the session server whether the player joined with the digest hash: its profile, or
// ErrUnverified (it answers 204, no content, for a join it does not know).
func authentication(session Session, name, hash string) (*Resp, error) {
	base, client := session.URL, session.Client
	if base == "" {
		base = "https://sessionserver.mojang.com"
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Get(base + "/session/minecraft/hasJoined?username=" + url.QueryEscape(name) + "&serverId=" + url.QueryEscape(hash))
	if err != nil {
		return nil, fmt.Errorf("auth: session server: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNoContent:
		return nil, ErrUnverified
	default:
		return nil, fmt.Errorf("auth: session server: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var Resp Resp
	if err := json.Unmarshal(body, &Resp); err != nil {
		return nil, fmt.Errorf("auth: session server's profile: %w", err)
	}
	return &Resp, nil
}

// authDigest computes a special SHA-1 digest required for Minecraft web
// authentication on Premium servers (online-mode=true).
// Source: https://minecraft.wiki/w/Java_Edition_protocol/Encryption
//
// Also many, many thanks to SirCmpwn and his wonderful gist (C#):
// https://gist.github.com/SirCmpwn/404223052379e82f91e6
func authDigest(serverID string, sharedSecret, publicKey []byte) string {
	h := sha1.New()
	h.Write([]byte(serverID))
	h.Write(sharedSecret)
	h.Write(publicKey)
	hash := h.Sum(nil)

	// Check for negative hashes
	negative := (hash[0] & 0x80) == 0x80
	if negative {
		hash = twosComplement(hash)
	}

	// Trim away zeroes
	res := strings.TrimLeft(fmt.Sprintf("%x", hash), "0")
	if negative {
		res = "-" + res
	}

	return res
}

// little endian
func twosComplement(p []byte) []byte {
	carry := true
	for i := len(p) - 1; i >= 0; i-- {
		p[i] = byte(^p[i])
		if carry {
			carry = p[i] == 0xff
			p[i]++
		}
	}
	return p
}

// Resp is the response of authentication
type Resp struct {
	Name       string
	ID         uuid.UUID
	Properties []user.Property
}

// Texture unmarshal the base64 encoded texture of Resp
func (r *Resp) Texture() (t user.Texture, err error) {
	var texture []byte
	texture, err = base64.StdEncoding.DecodeString(r.Properties[0].Value)
	if err != nil {
		return
	}

	err = json.Unmarshal(texture, &t)
	return
}
