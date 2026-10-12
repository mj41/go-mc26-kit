package main

import (
	"bufio"
	"io"
	"log"
	"strings"
	"sync"
)

var (
	consoleIn   io.Reader // what run reads its console from: os.Stdin, or a test's pipe
	consoleOnce sync.Once
)

// console forwards lines typed on the console to the server once the game has
// started: a line starting with "/" is sent as a command, anything else as
// chat. It is what lets a test (or a person) drive the bot.
func console() {
	consoleOnce.Do(func() {
		go func() {
			sc := bufio.NewScanner(consoleIn)
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if line == "" {
					continue
				}
				var err error
				if strings.HasPrefix(line, "/") {
					err = chatHandler.SendCommand(line[1:])
				} else {
					err = chatHandler.SendMessage(line)
				}
				if err != nil {
					log.Printf("console: %v", err)
				}
			}
		}()
	})
}
