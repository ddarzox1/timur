# Qauipsiz — команды разработки. Список: make help

-include .env
export

.DEFAULT_GOAL := help
.PHONY: help env up down logs db run test test-unit cover lint build psql reset

UNIT_PKGS := ./internal/domain/... ./internal/usecase/... ./internal/infra/... ./internal/config/...

help: ## Показать команды
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  \033[36mmake %-10s\033[0m %s\n", $$1, $$2}'

env: ## Создать .env из .env.example (существующий не трогает)
	@test -f .env && echo ".env уже есть" || (cp .env.example .env && echo "создан .env")

up: env ## Запустить всё в Docker: приложение + Postgres
	docker compose up -d --build
	@echo ""
	@echo "  Готово: http://localhost:8080"
	@code=$$(sed -n 's/^FIXED_OTP=//p' .env); echo "  Вход:   +7 701 000 00 01 (админ) или …02 (сотрудник), код $${code:-из логов: make logs}"

down: ## Остановить контейнеры (данные сохраняются)
	docker compose down

logs: ## Логи приложения
	docker compose logs -f app

db: ## Поднять только Postgres (для make run и тестов)
	docker compose up -d --wait db

run: env db ## Запустить приложение на хосте (go run) с переменными из .env
	go run ./cmd/server

test: db ## Все тесты, включая интеграционные с Postgres
	go test ./...

test-unit: ## Unit-тесты без базы
	go test $(UNIT_PKGS)

cover: ## Покрытие бизнес-логики
	go test -cover ./internal/usecase/...

lint: ## gofmt + go vet
	@out=$$(gofmt -l cmd internal web migrations); if [ -n "$$out" ]; then echo "Не отформатировано:"; echo "$$out"; exit 1; fi
	go vet ./...

build: ## Собрать бинарник в bin/qauipsiz
	go build -o bin/qauipsiz ./cmd/server

psql: ## Консоль Postgres
	docker compose exec db psql -U qauipsiz

reset: ## Удалить базу и загрузки (с подтверждением)
	@read -p "Удалить базу и все загруженные файлы? Введите yes: " a && [ "$$a" = yes ] || { echo "Отменено"; exit 1; }
	docker compose down -v
