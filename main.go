package main

import (
	"flag"
	_ "net/http/pprof"
	"time"

	log "github.com/liudanking/log4go"
)

var DEBUG bool

func main() {
	flag.BoolVar(&DEBUG, "d", false, "debug mode")
	mode := flag.String("m", "local", "mode: local or remote")
	laddr := flag.String("l", "127.0.0.1:9000", "local address")
	raddr := flag.String("r", "127.0.0.1:9001", "remote address")
	secure := flag.String("s", "01", "secure mode: 01: listen tcp, remote tls (remote/local)")
	cert := flag.String("c", "", "certificate (remote/local)")
	key := flag.String("k", "", "private key (remote/local)")
	tcount := flag.Int("t", 1, "tunnel count (local)")
	flag.Parse()

	// go func() {
	// 	var addr string
	// 	if *mode == "local" {
	// 		addr = "localhost:6060"
	// 	} else {
	// 		addr = "localhost:6061"
	// 	}
	// 	err := http.ListenAndServe(addr, nil)
	// 	if err != nil {
	// 		log.Error("pprof error:%v", err)
	// 	}
	// }()
	if DEBUG {
		log.AddFilter("stdout", log.DEBUG, log.NewConsoleLogWriter())
	} else {
		log.AddFilter("stdout", log.INFO, log.NewConsoleLogWriter())
	}
	ltls := (*secure)[0] == '1'
	rtls := (*secure)[1] == '1'

	switch *mode {
	case "local":
		localServer, err := NewLocalServer(*laddr, *raddr, ltls, rtls, *tcount)
		if err != nil {
			log.Error("NewLocalServer error:%v", err)
			break
		}
		localServer.Serve(*cert, *key)
	case "remote":
		remoteServer, err := NewRemoteServer(*laddr, *raddr, ltls, rtls)
		if err != nil {
			log.Error("NewRemoteServer error:%v", err)
			break
		}
		remoteServer.Serve(*cert, *key)
	}

	log.Info("exit")
	time.Sleep(2 * time.Second)
}
