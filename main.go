package main

import (
	"flag"
	"net/http"
	_ "net/http/pprof"

	log "github.com/liudanking/log4go"
)

func main() {
	mode := flag.String("m", "local", "mode: local or remote")
	laddr := flag.String("l", "127.0.0.1:9000", "local address")
	raddr := flag.String("r", "127.0.0.1:9001", "remote address")
	flag.Parse()

	go func() {
		var addr string
		if *mode == "local" {
			addr = "localhost:6060"
		} else {
			addr = "localhost:6061"
		}
		err := http.ListenAndServe(addr, nil)
		if err != nil {
			log.Error("pprof error:%v", err)
		}
	}()
	log.AddFilter("stdout", log.INFO, log.NewConsoleLogWriter())

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
