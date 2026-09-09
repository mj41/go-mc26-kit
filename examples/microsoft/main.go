package main

import (
	"context"
	"flag"
	"log"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/microsoft"
)

var (
	address  = flag.String("address", "", "Optional Minecraft server address, for example example.com:25565")
	clientID = flag.String("client-id", microsoft.DefaultClientID, "Microsoft client ID used for device-code auth")
	cacheDir = flag.String("cache-dir", "", "Optional cache directory for reusable Microsoft refresh tokens")
	cacheKey = flag.String("cache-key", "", "Optional cache key to separate multiple accounts in one cache directory")
	refresh  = flag.Bool("force-refresh", false, "Ignore cached Microsoft tokens and require a fresh browser login")
)

func main() {
	flag.Parse()

	access, err := microsoft.AuthenticateDeviceCode(context.Background(), microsoft.Options{
		ClientID:     *clientID,
		CacheDir:     *cacheDir,
		CacheKey:     *cacheKey,
		ForceRefresh: *refresh,
		OnDeviceCode: func(code microsoft.DeviceCode) {
			log.Printf("Open %s and enter code %s", code.VerificationURI, code.UserCode)
			log.Print(code.Message)
		},
		OnStatus: func(message string) {
			log.Printf("Microsoft auth: %s", message)
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Authenticated as %s (%s)", access.Profile.Name, access.Profile.ID)

	if *address == "" {
		return
	}

	client := bot.NewClient()
	client.Auth = bot.Auth{
		Name: access.Profile.Name,
		UUID: access.Profile.ID,
		AsTk: access.AccessToken(),
	}

	if err := client.JoinServer(*address); err != nil {
		log.Fatal(err)
	}
	log.Printf("Joined %s as %s", *address, access.Profile.Name)
	_ = client.Close()
}
