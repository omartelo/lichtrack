// Command youtrack-lich is a lich extension: the YouTrack board, issue list and
// card detail inside lich's window.
package main

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"time"

	"github.com/lichdotdev/lich/extension"

	"github.com/omartelo/youtrack-lich/youtrack"
)

func main() {
	dataDir := os.Getenv(extension.DataDirEnv)
	conn := extension.NewConn(os.Stdin, os.Stdout)
	a := &app{
		conn: conn, dataDir: dataDir, done: make(chan func()),
		query: defaultQuery,
		send: func(msg extension.Message) {
			if err := conn.Send(msg); err != nil {
				log.Printf("send: %v", err)
			}
		},
	}
	settings, err := LoadSettings(dataDir)
	if err != nil {
		log.Printf("settings: %v", err)
	}
	a.cardFields, a.boardID = settings.CardFields, settings.Board
	cfg, err := youtrack.LoadConfig(dataDir)
	switch {
	case err == nil:
		a.connect(cfg)
	case !errors.Is(err, fs.ErrNotExist):
		log.Printf("config: %v", err)
	}

	refresh := time.NewTicker(boardRefresh)
	defer refresh.Stop()
	for {
		select {
		case <-refresh.C:
			a.tick()
		case msg, ok := <-conn.Messages():
			if !ok {
				return
			}
			a.handle(msg)
		case apply := <-a.done:
			apply()
			a.loadAvatars()
			a.render()
		}
	}
}
