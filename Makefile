
build:
	@echo "building..." && \
	go build && \
	echo "Done!"


run-local: build
	./gotunnel -d -m local -l 127.0.0.1:9000 -r tls-local.618033988.cc:9101 -t 1

run-local-proxy: build
	./gotunnel -d -m local -l 127.0.0.1:9000 -r gt-jp2.618033988.cc:9101 -t 2

run-remote: build
	./gotunnel -d -m remote -l 127.0.0.1:9001 -r 106.185.47.240:80 -c "/Users/liudanking/Documents/config/ssh-key/ngg/star_cert_combine.pem" -k "/Users/liudanking/Documents/config/ssh-key/ngg/star_618033988_cc.key"