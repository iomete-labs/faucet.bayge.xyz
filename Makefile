
MIGRATIONS := $(shell find migrations -type f)

.PHONY: build

all: build

build: main.db main.out

main.db: ${MIGRATIONS}
	@cat ${MIGRATIONS} | sqlite3 main.db

main.out: main.go database.go
	@go build -o main.out
