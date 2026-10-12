package bot

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/net"
	"github.com/mj41/go-mc26/net/CFB8"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/login"
)

// LoginErr is a failure of JoinServer before the configuration phase: Stage
// says where ("connect server", "disconnect", "encrypt start", …).
type LoginErr struct {
	Stage string
	Err   error
}

func (l LoginErr) Error() string {
	return "bot: login error: [" + l.Stage + "] " + l.Err.Error()
}

func (l LoginErr) Unwrap() error {
	return l.Err
}

func (c *Client) joinLogin(conn *net.Conn, options JoinOptions) error {
	var err error
	if c.Auth.UUID != "" {
		c.UUID, err = uuid.Parse(c.Auth.UUID)
		if err != nil {
			return LoginErr{"login start", err}
		}
	}
	hello := login.ServerboundHello{Name: pk.String(c.Auth.Name), ProfileID: pk.UUID(c.UUID)}
	if err = conn.WritePacket(pk.Marshal(hello.PacketID(), hello)); err != nil {
		return LoginErr{"login start", err}
	}
	receiving := "encrypt start"
	for {
		// Receive Packet
		var p pk.Packet
		if err = conn.ReadPacket(&p); err != nil {
			return LoginErr{receiving, err}
		}

		// Handle Packet
		switch packetid.ClientboundPacketID(p.ID) {
		case packetid.ClientboundLoginLoginDisconnect: // LoginDisconnect
			var d login.LoginDisconnect
			if err = p.Scan(&d); err != nil {
				return LoginErr{"disconnect", err}
			}
			var reason chat.Message
			if err = json.Unmarshal([]byte(d.Reason), &reason); err != nil {
				return LoginErr{"disconnect", err}
			}
			return LoginErr{"disconnect", DisconnectErr(reason)}

		case packetid.ClientboundLoginHello: // Encryption Request
			if err := handleEncryptionRequest(conn, c, p, options); err != nil {
				return LoginErr{"encryption", err}
			}
			receiving = "set compression"

		case packetid.ClientboundLoginLoginFinished: // Login Success
			var f login.LoginFinished
			if err := p.Scan(&f); err != nil {
				return LoginErr{"login success", err}
			}
			c.UUID = uuid.UUID(f.GameProfile.ID)
			c.Name = string(f.GameProfile.Name)
			ack := login.LoginAcknowledged{}
			if err = conn.WritePacket(pk.Marshal(ack.PacketID(), ack)); err != nil {
				return LoginErr{"login success", err}
			}
			return nil

		case packetid.ClientboundLoginLoginCompression: // Set Compression
			var comp login.LoginCompression
			if err := p.Scan(&comp); err != nil {
				return LoginErr{"compression", err}
			}
			conn.SetThreshold(int(comp.CompressionThreshold))
			receiving = "login success"

		case packetid.ClientboundLoginCustomQuery: // Login Plugin Request
			var q login.CustomQuery
			if err := p.Scan(&q); err != nil {
				return LoginErr{"Login Plugin", err}
			}
			answer := login.CustomQueryAnswer{TransactionID: q.TransactionID}
			if handler, ok := c.LoginPlugin[string(q.Identifier)]; ok {
				data, err := handler(pk.PluginMessageData(q.Payload))
				if err != nil {
					return LoginErr{"Login Plugin", err}
				}
				answer.Payload.Has = true
				answer.Payload.Val = data
			}
			if err := conn.WritePacket(pk.Marshal(answer.PacketID(), answer)); err != nil {
				return LoginErr{"login Plugin", err}
			}

		case packetid.ClientboundLoginCookieRequest:
			var req login.CookieRequest
			if err := p.Scan(&req); err != nil {
				return LoginErr{"cookie request", err}
			}
			cookieContent := c.Cookies[string(req.Key)]
			resp := login.CookieResponse{Key: req.Key}
			resp.Payload.Has = cookieContent != nil
			resp.Payload.Val = pk.ByteArray(cookieContent)
			if err = conn.WritePacket(pk.Marshal(resp.PacketID(), resp)); err != nil {
				return LoginErr{"cookie response", err}
			}
		}
	}
}

// Auth is the player a client joins as. An offline-mode server needs only Name
// (no call to Mojang is made); an online-mode server needs all three, which
// package microsoft gives.
type Auth struct {
	Name string // the player's name
	UUID string // the profile id, hex, with or without dashes; empty in offline mode
	AsTk string // the Minecraft access token; empty in offline mode
}

func handleEncryptionRequest(conn *net.Conn, c *Client, p pk.Packet, options JoinOptions) error {
	// 创建AES对称加密密钥
	key, encoStream, decoStream := newSymmetricEncryption()

	// Read EncryptionRequest
	var er login.ClientboundHello
	if err := p.Scan(&er); err != nil {
		return err
	}

	// the session server is told of the join only when the server asks (an online-mode one), as the game's
	// client does (ClientHandshakePacketListenerImpl: shouldAuthenticate)
	if er.ShouldAuthenticate {
		if err := loginAuth(c.Auth, key, er, options); err != nil {
			return fmt.Errorf("login fail: %v", err)
		}
	}

	// 响应加密请求
	// Write Encryption Key Response
	p, err := genEncryptionKeyResponse(key, er.PublicKey, er.Challenge)
	if err != nil {
		return fmt.Errorf("gen encryption key response fail: %v", err)
	}

	err = conn.WritePacket(p)
	if err != nil {
		return err
	}

	// 设置连接加密
	conn.SetCipher(encoStream, decoStream)
	return nil
}

// authDigest computes a special SHA-1 digest required for Minecraft web
// authentication on Premium servers (online-mode=true).
// Source: https://minecraft.wiki/w/Java_Edition_protocol/Encryption
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
	res := strings.TrimLeft(hex.EncodeToString(hash), "0")
	if negative {
		res = "-" + res
	}

	return res
}

// little endian
func twosComplement(p []byte) []byte {
	carry := true
	for i := len(p) - 1; i >= 0; i-- {
		p[i] = ^p[i]
		if carry {
			carry = p[i] == 0xff
			p[i]++
		}
	}
	return p
}

type profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type request struct {
	AccessToken     string  `json:"accessToken"`
	SelectedProfile profile `json:"selectedProfile"`
	ServerID        string  `json:"serverId"`
}

func loginAuth(auth Auth, shareSecret []byte, er login.ClientboundHello, options JoinOptions) error {
	digest := authDigest(string(er.ServerID), shareSecret, er.PublicKey)

	requestPacket, err := json.Marshal(
		request{
			AccessToken: auth.AsTk,
			SelectedProfile: profile{
				ID:   auth.UUID,
				Name: auth.Name,
			},
			ServerID: digest,
		},
	)
	if err != nil {
		return fmt.Errorf("create request packet to yggdrasil faile: %v", err)
	}

	session, client := options.SessionServer, options.HTTPClient
	if session == "" {
		session = "https://sessionserver.mojang.com"
	}
	if client == nil {
		client = http.DefaultClient
	}
	PostRequest, err := http.NewRequest(http.MethodPost, session+"/session/minecraft/join", bytes.NewReader(requestPacket))
	if err != nil {
		return fmt.Errorf("make request error: %v", err)
	}
	PostRequest.Header.Set("User-agent", "go-mc")
	PostRequest.Header.Set("Connection", "keep-alive")
	PostRequest.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(PostRequest)
	if err != nil {
		return fmt.Errorf("post fail: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("auth fail: %s", string(body))
	}
	return nil
}

// AES/CFB8 with random key
func newSymmetricEncryption() (key []byte, encoStream, decoStream cipher.Stream) {
	key = make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}

	b, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	decoStream = CFB8.NewCFB8Decrypt(b, key)
	encoStream = CFB8.NewCFB8Encrypt(b, key)
	return
}

func genEncryptionKeyResponse(shareSecret, publicKey, verifyToken []byte) (erp pk.Packet, err error) {
	iPK, err := x509.ParsePKIXPublicKey(publicKey) // Decode Public Key
	if err != nil {
		err = fmt.Errorf("decode public key fail: %v", err)
		return
	}
	rsaKey := iPK.(*rsa.PublicKey)
	cryptPK, err := rsa.EncryptPKCS1v15(rand.Reader, rsaKey, shareSecret)
	if err != nil {
		err = fmt.Errorf("encryption share secret fail: %v", err)
		return
	}

	verifyT, err := rsa.EncryptPKCS1v15(rand.Reader, rsaKey, verifyToken)
	if err != nil {
		err = fmt.Errorf("encryption verfy tokenfail: %v", err)
		return erp, err
	}
	key := login.Key{Keybytes: pk.ByteArray(cryptPK), EncryptedChallenge: pk.ByteArray(verifyT)}
	return pk.Marshal(key.PacketID(), key), nil
}
