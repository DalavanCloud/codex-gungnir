DEFAULT: build

GO           ?= go
GOFMT        ?= $(GO)fmt
FIRST_GOPATH := $(firstword $(subst :, ,$(shell $(GO) env GOPATH)))
DEP          := $(FIRST_GOPATH)/bin/dep
GUNGNIR    := $(FIRST_GOPATH)/bin/gungnir

PROGVER = $(shell grep 'applicationVersion.*= ' main.go | awk '{print $$3}' | sed -e 's/\"//g')

.PHONY: $(DEP)
$(DEP):
	GOOS= GOARCH= $(GO) get -u github.com/golang/dep/...

.PHONY: deps
deps: $(DEP)
	$(DEP) ensure

.PHONY: build
build: deps
	$(GO) build

rpm:
	mkdir -p ./OPATH/SOURCES
	tar -czvf ./OPATH/SOURCES/gungnir-$(PROGVER).tar.gz . --exclude ./.git --exclude ./OPATH --exclude ./conf --exclude ./deploy --exclude ./vendor
	cp conf/gungnir.service ./OPATH/SOURCES/
	cp conf/gungnir.yaml  ./OPATH/SOURCES/
	rpmbuild --define "_topdir $(CURDIR)/OPATH" \
    		--define "_version $(PROGVER)" \
    		--define "_release 1" \
    		-ba deploy/packaging/gungnir.spec

.PHONY: version
version:
	@echo $(PROGVER)


# If the first argument is "update-version"...
ifeq (update-version,$(firstword $(MAKECMDGOALS)))
  # use the rest as arguments for "update-version"
  RUN_ARGS := $(wordlist 2,$(words $(MAKECMDGOALS)),$(MAKECMDGOALS))
  # ...and turn them into do-nothing targets
  $(eval $(RUN_ARGS):;@:)
endif

.PHONY: update-version
update-version:
	@echo "Update Version $(PROGVER) to $(RUN_ARGS)"
	sed -i "s/$(PROGVER)/$(RUN_ARGS)/g" main.go


.PHONY: install
install: deps
	echo go build -o $(GUNGNIR) $(PROGVER)

.PHONY: release-artifacts
release-artifacts:
	GOOS=darwin GOARCH=amd64 go build -o ./OPATH/gungnir-$(PROGVER).darwin-amd64 && gzip -f ./OPATH/gungnir-$(PROGVER).darwin-amd64
	GOOS=linux  GOARCH=amd64 go build -o ./OPATH/gungnir-$(PROGVER).linux-amd64  && gzip -f ./OPATH/gungnir-$(PROGVER).linux-amd64

.PHONY: docker
docker:
	docker build -f ./deploy/Dockerfile -t gungnir:$(PROGVER) .

# build docker without running dep ensure
.PHONY: local-docker
local-docker:
	docker build -f ./deploy/Dockerfile.local -t gungnir:local .

.PHONY: style
style:
	! gofmt -d $$(find . -path ./vendor -prune -o -name '*.go' -print) | grep '^'

.PHONY: test
test: deps
	go test -o $(GUNGNIR) -v -race  -coverprofile=cover.out $(go list ./... | grep -v "/vendor/")

.PHONY: test-cover
test-cover: test
	go tool cover -html=cover.out

.PHONY: codecov
codecov: test
	curl -s https://codecov.io/bash | bash

.PHONEY: it
it:
	./it.sh

.PHONY: clean
clean:
	rm -rf ./codex-gungnir ./OPATH ./coverage.txt ./vendor