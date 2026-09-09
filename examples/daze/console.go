package main

import (
	"bufio"
	"log"
	"os"
	"strings"
	"sync"
)

var consoleOnce sync.Once

// console forwards lines typed on stdin to the server once the game has
// started: a line starting with "/" is sent as a command, anything else as
// chat. It is what lets a test (or a person) drive the bot.
func console() {
	consoleOnce.Do(func() {
		go func() {
			sc := bufio.NewScanner(os.Stdin)
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
