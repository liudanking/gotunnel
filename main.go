package main

import (
	"flag"
	_ "net/http/pprof"

	log "github.com/liudanking/log4go"
)

func main() {
	mode := flag.String("m", "local", "mode: local or remote")
	laddr := flag.String("l", "127.0.0.1:9000", "local address")
	raddr := flag.String("r", "127.0.0.1:9001", "remote address")
	flag.Parse()

	// go func() {
	// 	err := http.ListenAndServe("localhost:6060", nil)
	// 	if err != nil {
	// 		log.Error("pprof error:%v", err)
	// 	}
	// }()
	log.AddFilter("stdout", log.DEBUG, log.NewConsoleLogWriter())

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
