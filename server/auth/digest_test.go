package auth

import "testing"

// The digests of the protocol's documentation (minecraft.wiki, Java Edition protocol/Encryption): the SHA-1
// of the name alone, printed as a signed number without leading zeros — jeb_'s negative (two's complement),
// simon's with a leading zero dropped.
func TestAuthDigest(t *testing.T) {
	for name, want := range map[string]string{
		"Notch": "4ed1f46bbe04bc756bcb17c0c7ce3e4632f06a48",
		"jeb_":  "-7c9d5b0044c130109a5d7b5fb5c317c02b4e28c1",
		"simon": "88e16a1019277b15d58faf0541e11910eb756f6",
	} {
		if got := authDigest(name, nil, nil); got != want {
			t.Errorf("authDigest(%q) = %s, want %s", name, got, want)
		}
	}
}
