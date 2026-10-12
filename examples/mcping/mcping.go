// Mcping asks a server for its entry in the server list, as the multiplayer screen does, and prints it: the
// version, the description, the round trip, the players.
//
//	go run ./examples/mcping localhost:25565
//	go run ./examples/mcping -f icon.png localhost   # also saves the server's icon
//
// mcping_test.go runs it against a fake server (examples/README.md).
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/chat"
)

var favicon = flag.String("f", "", "save the server's icon to this PNG file")

type status struct {
	Description chat.Message
	Players     struct {
		Max    int
		Online int
		Sample []struct {
			ID   uuid.UUID
			Name string
		}
	}
	Version struct {
		Name     string
		Protocol int
	}
	Favicon Icon
	Delay   time.Duration
}

// Icon should be a PNG image that is Base64 encoded
// (without newlines: \n, new lines no longer work since 1.13)
// and prepended with "data:image/png;base64,".
type Icon string

func (i Icon) ToImage() (icon image.Image, err error) {
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(string(i), prefix) {
		return nil, fmt.Errorf("server icon should prepended with %q", prefix)
	}
	base64png := strings.TrimPrefix(string(i), prefix)
	r := base64.NewDecoder(base64.StdEncoding, strings.NewReader(base64png))
	icon, err = png.Decode(r)
	return
}

var outTemp = template.Must(template.New("output").Parse(`
	Version: [{{ .Version.Protocol }}] {{ .Version.Name }}
	Description: 
{{ .Description }}
	Delay: {{ .Delay }}
	Players: {{ .Players.Online }}/{{ .Players.Max }}{{ range .Players.Sample }}
	- [{{ .Name }}] {{ .ID }}{{ end }}
`))

func (s *status) String() string {
	var sb strings.Builder
	err := outTemp.Execute(&sb, s)
	if err != nil {
		panic(err)
	}
	return sb.String()
}

func usage() {
	_, _ = fmt.Fprintf(flag.CommandLine.Output(), "Usage:\n%s [-f icon.png] <address>[:port]\n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	flag.Usage = usage
	flag.Parse()
	addr := flag.Arg(0)
	if addr == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(addr, *favicon, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run pings addr and prints its status to w; with icon set, it saves the server's icon there.
func run(addr, icon string, w io.Writer) error {
	fmt.Fprintf(w, "MCPING (%s):", addr)
	resp, delay, err := bot.PingAndList(addr)
	if err != nil {
		return fmt.Errorf("ping and list server fail: %w", err)
	}
	var s status
	if err := json.Unmarshal(resp, &s); err != nil {
		return fmt.Errorf("parse json response fail: %w", err)
	}
	s.Delay = delay
	fmt.Fprint(w, &s)
	if icon == "" {
		return nil
	}
	img, err := s.Favicon.ToImage()
	if err != nil {
		return err
	}
	f, err := os.Create(icon)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
