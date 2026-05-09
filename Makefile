SHELL := /bin/bash

GO              ?= go
GOPATH_BIN      := $(shell $(GO) env GOPATH)/bin
PROTOC          ?= protoc
PROTO_DIR       := proto
GEN_DIR         := gen
GOOGLEAPIS_DIR  := third_party/googleapis

PROTO_FILES := $(shell find $(PROTO_DIR) -name '*.proto')

.PHONY: all proto tidy build test docker tools clean

all: proto build

tools:
	$(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2
	$(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
	$(GO) install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@v2.22.0
	$(GO) install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@v2.22.0

proto:
	@mkdir -p $(GEN_DIR)
	PATH="$(GOPATH_BIN):$$PATH" $(PROTOC) \
		-I $(PROTO_DIR) \
		-I $(GOOGLEAPIS_DIR) \
		--go_out=$(GEN_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) --go-grpc_opt=paths=source_relative \
		--grpc-gateway_out=$(GEN_DIR) --grpc-gateway_opt=paths=source_relative \
		--grpc-gateway_opt=generate_unbound_methods=true \
		--openapiv2_out=$(GEN_DIR) --openapiv2_opt=use_go_templates=true,json_names_for_fields=false \
		$(PROTO_FILES)
	cp $(GEN_DIR)/adsb/v2/adsb.swagger.json internal/docs/adsb.swagger.json

tidy:
	$(GO) mod tidy

build:
	$(GO) build -o server ./cmd/server
	$(GO) build -o sbs-feeder ./cmd/sbs-feeder

test:
	$(GO) test ./...

run:
	$(GO) run ./cmd/server

docker:
	docker build -t dump1090-adsblol:latest .

clean:
	rm -rf $(GEN_DIR)/adsb
