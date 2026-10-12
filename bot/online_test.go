package bot_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/google/uuid"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/simworld/memnet"
	"github.com/mj41/go-mc26-kit/server"
	"github.com/mj41/go-mc26-kit/server/auth"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/version"
	mcnet "github.com/mj41/go-mc26/net"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/configuration"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/yggdrasil/user"
)

// fakeSession is Mojang's session server for a test: the client's join (with its access token) recorded by
// the digest it gives, the server's hasJoined answered from those records — the profile, or 204 for a join
// it does not have.
type fakeSession struct {
	tokens map[string]string // name → the access token its account has

	mu     sync.Mutex
	joined map[[2]string]uuid.UUID // (name, digest) → the account
	joins  []string                // the digests clients joined with, in order
	asks   []string                // the digests servers asked about, in order
}

func (f *fakeSession) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/session/minecraft/join":
		var req struct {
			AccessToken     string `json:"accessToken"`
			SelectedProfile struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"selectedProfile"`
			ServerID string `json:"serverId"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || f.tokens[req.SelectedProfile.Name] != req.AccessToken {
			http.Error(w, `{"error":"ForbiddenOperationException"}`, http.StatusForbidden)
			return
		}
		id, err := uuid.Parse(req.SelectedProfile.ID)
		if err != nil {
			http.Error(w, "bad profile id", http.StatusBadRequest)
			return
		}
		if f.joined == nil {
			f.joined = map[[2]string]uuid.UUID{}
		}
		f.joined[[2]string{req.SelectedProfile.Name, req.ServerID}] = id
		f.joins = append(f.joins, req.ServerID)
		w.WriteHeader(http.StatusNoContent)
	case "/session/minecraft/hasJoined":
		name, digest := r.URL.Query().Get("username"), r.URL.Query().Get("serverId")
		f.asks = append(f.asks, digest)
		id, ok := f.joined[[2]string{name, digest}]
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": strings.ReplaceAll(id.String(), "-", ""), "name": name, "properties": []any{}})
	default:
		http.NotFound(w, r)
	}
}

// client is an http.Client served by f in the process: no socket, so a test in a bubble can use it.
func (f *fakeSession) client() *http.Client { return &http.Client{Transport: handlerTransport{f}} }

type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, r)
	return rec.Result(), nil
}

// big is text longer than any threshold: it travels compressed, and encrypted.
var big = strings.Repeat("the quick brown fox ", 150)

// onlineGame is the play state of the online server: who came in (as the session server says), a long system
// message sent, the client's chat read back.
type onlineGame struct {
	in   chan [2]string // name, id
	said chan string
}

func (g *onlineGame) AcceptPlayer(name string, id uuid.UUID, _ *user.PublicKey, _ []user.Property, _ int32, conn *mcnet.Conn) {
	g.in <- [2]string{name, id.String()}
	sc := play.SystemChat{Content: chat.Text(big)}
	if conn.WritePacket(pk.Marshal(sc.PacketID(), sc)) != nil {
		return
	}
	for {
		var p pk.Packet
		if conn.ReadPacket(&p) != nil {
			return
		}
		if packetid.ServerboundPacketID(p.ID) == packetid.ServerboundPlayChat {
			var c play.Chat
			if p.Scan(&c) == nil {
				g.said <- string(c.Message)
			}
			return
		}
	}
}

// finishConfig ends the configuration at once: the test is about the login.
type finishConfig struct{}

func (finishConfig) AcceptConfig(conn *mcnet.Conn) error {
	f := configuration.ClientboundFinishConfiguration{}
	return conn.WritePacket(pk.Marshal(f.PacketID(), f))
}

// onlineServer is the kit's server in online mode on memory, its session server sess, compressing every
// packet (threshold 0).
func onlineServer(t *testing.T, sess *fakeSession) (*memnet.Listener, *onlineGame) {
	l := memnet.Listen("online")
	g := &onlineGame{in: make(chan [2]string, 1), said: make(chan string, 1)}
	players := server.NewPlayerList(10)
	srv := &server.Server{
		ListPingHandler: struct {
			*server.PingInfo
			*server.PlayerList
		}{server.NewPingInfo("online", version.ProtocolVersion, chat.Text("online"), nil), players},
		LoginHandler: &server.MojangLoginHandler{OnlineMode: true, Threshold: 0, LoginChecker: players,
			Session: auth.Session{URL: "http://session.test", Client: sess.client()}},
		ConfigHandler: finishConfig{},
		GamePlay:      g,
	}
	mc := l.MC()
	go func() {
		for {
			conn, err := mc.Accept()
			if err != nil {
				return
			}
			go srv.AcceptConn(&conn)
		}
	}()
	t.Cleanup(func() { _ = mc.Close() })
	return l, g
}

// joinOnline logs a client in as name with token, its session server sess.
func joinOnline(l *memnet.Listener, sess *fakeSession, name string, id uuid.UUID, token string) (*bot.Client, chan string, error) {
	c := bot.NewClient()
	c.Auth = bot.Auth{Name: name, UUID: id.String(), AsTk: token}
	heard := make(chan string, 1)
	c.Events.AddListener(bot.PacketHandler{ID: packetid.ClientboundPlaySystemChat, F: func(p pk.Packet) error {
		var sc play.SystemChat
		if err := p.Scan(&sc); err != nil {
			return err
		}
		heard <- chat.Message(sc.Content).ClearString()
		return nil
	}})
	err := c.JoinServerWithOptions("online", bot.JoinOptions{MCDialer: memnet.Dialer{L: l},
		SessionServer: "http://session.test", HTTPClient: sess.client()})
	return c, heard, err
}

// An online-mode login, client and server of this kit, the session server a fake: the client joins with the
// digest it computes, the server verifies the one it computes, so they meet only when the two agree. Then the
// connection, encrypted (AES/CFB8) and compressed (threshold 0), carries a long message each way intact.
// 32 logins: each digest is negative half the time, so the two's-complement case is met (all but surely).
func TestOnlineLogin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		steve := uuid.MustParse("d0e05de7-6067-454d-beae-c6d19d886191")
		sess := &fakeSession{tokens: map[string]string{"Steve": "token-of-steve"}}
		l, g := onlineServer(t, sess)
		for i := 0; i < 32; i++ {
			c, heard, err := joinOnline(l, sess, "Steve", steve, "token-of-steve")
			if err != nil {
				t.Fatalf("login %d: %v (joined with %v, asked about %v)", i, err, sess.joins, sess.asks)
			}
			if in := <-g.in; in != [2]string{"Steve", steve.String()} {
				t.Fatalf("login %d: the server let in %v, want Steve's account", i, in)
			}
			done := make(chan struct{})
			go func() { _ = c.HandleGame(); close(done) }()
			if got := <-heard; got != big {
				t.Fatalf("login %d: the server's message came as %d characters, want %d", i, len(got), len(big))
			}
			msg := big[:250] // a chat message's most
			ch := play.Chat{Message: pk.String(msg)}
			if err := c.Conn.WritePacket(pk.Marshal(ch.PacketID(), ch)); err != nil {
				t.Fatal(err)
			}
			if got := <-g.said; got != msg {
				t.Fatalf("login %d: the client's chat came as %q", i, got)
			}
			_ = c.Close()
			<-done
		}
		negative := 0
		for i, d := range sess.joins {
			if sess.asks[i] != d {
				t.Errorf("login %d: the client's digest %s, the server's %s", i, d, sess.asks[i])
			}
			if strings.HasPrefix(d, "-") {
				negative++
			}
		}
		if negative == 0 {
			t.Errorf("no negative digest in %d logins: the two's-complement case untested", len(sess.joins))
		}
	})
}

// The client joined at one session server, the server verifies at another that has no record of it
// (hasJoined answers 204): the client is told what the game tells it, "Failed to verify username!".
func TestOnlineLoginUnverified(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		steve := uuid.MustParse("d0e05de7-6067-454d-beae-c6d19d886191")
		theServers := &fakeSession{}
		l, _ := onlineServer(t, theServers)
		theClients := &fakeSession{tokens: map[string]string{"Steve": "token-of-steve"}}
		_, _, err := joinOnline(l, theClients, "Steve", steve, "token-of-steve")
		var kicked bot.DisconnectErr
		if !errors.As(err, &kicked) || !strings.Contains(chat.Message(kicked).ClearString(), "Failed to verify username!") {
			t.Fatalf("the login: %v, want the server's \"Failed to verify username!\"", err)
		}
	})
}

// A token the session server refuses: the client says so and leaves, the server is not left waiting.
func TestOnlineLoginBadToken(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		steve := uuid.MustParse("d0e05de7-6067-454d-beae-c6d19d886191")
		sess := &fakeSession{tokens: map[string]string{"Steve": "token-of-steve"}}
		l, _ := onlineServer(t, sess)
		_, _, err := joinOnline(l, sess, "Steve", steve, "a stale token")
		if err == nil || !strings.Contains(err.Error(), "ForbiddenOperationException") {
			t.Fatalf("the login: %v, want the session server's refusal", err)
		}
	})
}
