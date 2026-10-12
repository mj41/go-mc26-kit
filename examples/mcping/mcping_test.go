package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/data/version"
)

// mcping prints a server's list entry: the fake server's version, description and players. (On loopback, not
// in a bubble: the ping dials an address, as the example does.)
func TestMcping(t *testing.T) {
	s := fakeserver.Start(t)
	var out strings.Builder
	if err := run(s.Addr, "", &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		fmt.Sprintf("Version: [%d] fake", version.ProtocolVersion),
		"a test's server",
		"Players: 0/1",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the output has no %q:\n%s", want, out.String())
		}
	}
}
