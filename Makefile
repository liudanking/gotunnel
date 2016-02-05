
build:
	@echo "building..." && \
	go build && \
	echo "Done!"


run-local: build
	./gotunnel -d -m local -l 127.0.0.1:9000 -r 127.0.0.1:9001

run-remote: build
	./gotunnel -d -m remote -l 127.0.0.1:9001 -r 106.185.47.240:80