package main

import (
	"flag"

	log "github.com/liudanking/log4go"
)

func main() {
	mode := flag.String("m", "local", "mode: local or remote")
	laddr := flag.String("l", "127.0.0.1:9000", "local address")
	raddr := flag.String("r", "127.0.0.1:9001", "remote address")
	flag.Parse()

	switch *mode {
	case "local":
		localServer, err := NewLocalServer(*laddr, *raddr)
		if err != nil {
			log.Error("NewLocalServer error:%v", err)
			return
		}
		localServer.serve()
	case "remote":
		remoteServer, err := NewRemoteServer(*laddr, *raddr)
		if err != nil {
			log.Error("NewRemoteServer error:%v", err)
			return
		}
		remoteServer.serve()
	}

	log.Info("exit")
}
