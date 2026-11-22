# ASCII site: C++ engine -> Python builder -> Go server
#
#   make            build everything (engine, site, server binary)
#   make run        build and serve on http://localhost:8080
#   make test       C++ self-test, Python tests, Go tests, lint
#   make clean      remove build outputs

CXX      ?= c++
CXXFLAGS ?= -std=c++17 -O2 -Wall -Wextra -Wpedantic
PYTHON   ?= python3
GO       ?= go
PORT     ?= 8080
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)

ENGINE_SRC := $(wildcard engine/*.cpp engine/*.hpp)
SITE_SRC   := content/site.json web/app.js web/template.html builder/build.py $(wildcard lab/*.html)
SERVER_SRC := $(wildcard server/*.go) server/go.mod

.PHONY: all engine site server run test lint clean

all: server

engine: bin/asciigen

bin/asciigen: $(ENGINE_SRC)
	@mkdir -p bin
	$(CXX) $(CXXFLAGS) -o $@ $(filter %.cpp,$(ENGINE_SRC))

site: dist/index.html

dist/index.html: bin/asciigen $(SITE_SRC)
	$(PYTHON) builder/build.py
	$(PYTHON) builder/lint.py

server: bin/site-server

bin/site-server: dist/index.html $(SERVER_SRC)
	@find server/web -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
	cp -R dist/. server/web/
	cd server && CGO_ENABLED=0 $(GO) build -trimpath \
		-ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)" \
		-o ../bin/site-server .

run: server
	PORT=$(PORT) ./bin/site-server

lint: site
	$(PYTHON) builder/lint.py

test: bin/asciigen
	./bin/asciigen --self-test
	$(PYTHON) -m unittest discover -s tests -p 'test_*.py' -v
	cd server && $(GO) vet ./... && $(GO) test ./...
	$(MAKE) lint

clean:
	rm -rf bin build dist tests/frames
	@find server/web -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
