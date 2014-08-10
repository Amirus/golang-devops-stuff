# Copyright 2014 tsuru authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

define HG_ERROR

FATAL: You need Mercurial (hg) to download tsuru dependencies.
       For more details, please check
       http://docs.tsuru.io/en/latest/contribute/setting-up-your-tsuru-development-environment.html#installing-git-bzr-and-mercurial
endef

define GIT_ERROR

FATAL: You need Git to download tsuru dependencies.
       For more details, please check
       http://docs.tsuru.io/en/latest/contribute/setting-up-your-tsuru-development-environment.html#installing-git-bzr-and-mercurial
endef

define BZR_ERROR

FATAL: You need Bazaar (bzr) to download tsuru dependencies.
       For more details, please check
       http://docs.tsuru.io/en/latest/contribute/setting-up-your-tsuru-development-environment.html#installing-git-bzr-and-mercurial
endef

.PHONY: all check-path get hg git bzr get-code test race

all: check-path get test

# It does not support GOPATH with multiple paths.
check-path:
ifndef GOPATH
	@echo "FATAL: you must declare GOPATH environment variable, for more"
	@echo "       details, please check"
	@echo "       http://golang.org/doc/code.html#GOPATH"
	@exit 1
endif
ifneq ($(subst ~,$(HOME),$(GOPATH))/src/github.com/tsuru/tsuru, $(PWD))
	@echo "FATAL: you must clone tsuru inside your GOPATH To do so,"
	@echo "       you can run go get github.com/tsuru/tsuru/..."
	@echo "       or clone it manually to the dir $(GOPATH)/src/github.com/tsuru/tsuru"
	@exit 1
endif
	@exit 0

get: hg git bzr get-code godep

hg:
	$(if $(shell hg), , $(error $(HG_ERROR)))

git:
	$(if $(shell git), , $(error $(GIT_ERROR)))

bzr:
	$(if $(shell bzr), , $(error $(BZR_ERROR)))

get-code:
	go get $(GO_EXTRAFLAGS) -u -d -t ./...

godep:
	go get $(GO_EXTRAFLAGS) github.com/tools/godep
	godep restore ./...

_go_test:
	go clean $(GO_EXTRAFLAGS) ./...
	go test $(GO_EXTRAFLAGS) ./...

_tsr_dry:
	go build $(GO_EXTRAFLAGS) -o tsr ./cmd/tsr
	./tsr api --dry --config ./etc/tsuru.conf
	rm -f tsr

_sh_tests:
	@misc/test-hooks.bash

test: _go_test _tsr_dry _sh_tests

_install_deadcode: git
	go get $(GO_EXTRAFLAGS) github.com/remyoudompheng/go-misc/deadcode

deadcode: _install_deadcode
	@go list ./... | sed -e 's;github.com/tsuru/tsuru/;;' | xargs deadcode

deadc0de: deadcode

race:
	go test $(GO_EXTRAFLAGS) -race -i ./...
	go test $(GO_EXTRAFLAGS) -race ./...

doc:
	@cd docs && make html SPHINXOPTS="-N -W"
