fmt:
	go fmt ./...

lint:
	golangci-lint run

tidy:
	go mod tidy

test e2e:
	docker compose --env-file .env.test -f docker-compose.test.yaml up -d --build
	go test ./tests -v -count=1 ; \
	EXIT_CODE=$$? ; \
	docker compose -f docker-compose.test.yaml down -v ; \
	exit $$EXIT_CODE

loadtest:
	docker compose --env-file .env.test -f docker-compose.test.yaml up -d --build
	go run cmd/loadtest/main.go --cmd=setup && \
	go run cmd/loadtest/main.go --cmd=test ; \
	EXIT_CODE=$$? ; \
	docker compose -f docker-compose.test.yaml down -v ; \
	exit $$EXIT_CODE

gen:
	go tool ogen --target internal/api --config openapi/ogen.yaml --clean openapi/openapi.yaml

up:
	docker compose up -d

up-build:
	docker compose up -d --build

down:
	docker compose down

restart:
	docker compose down -v
	docker compose up -d --build