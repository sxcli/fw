package main

import (
	"log"

	"sxcli.dev/fw/conf"
)

type Cfg struct {
	File string `json:"file-name" conf:"file-name,f" env:"-"`
}

func main() {
	l, handled := conf.New("confbox", &Cfg{})
	if handled {
		return
	}
	_, err := l.Load()
	if err != nil {
		log.Fatal(err)
	}

	l2, handled := conf.Builder("confbox").Section("base", &Cfg{}).Suppress("name").Build()
	if handled {
		return
	}

	if _, err = l2.Load(); err != nil {
		log.Fatal(err)
	}
}
