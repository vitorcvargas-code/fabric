.PHONY: build test tidy clean

BIN := fabric

build:
	go build -o $(BIN) ./cmd/fabric

tidy:
	go mod tidy

test:
	go test ./...

clean:
	rm -f $(BIN)
