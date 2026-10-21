# ---------- build ----------
FROM golang:1.24-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
# сначала только манифесты зависимостей — слой с модулями кэшируется между сборками
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN go test ./... \
 && go build -ldflags="-s -w" -o /out/api ./cmd/api \
 && go build -ldflags="-s -w" -o /out/selfcheck ./cmd/selfcheck \
 && mkdir -p /out/data

# ---------- runtime ----------
# distroless: нет shell и пакетного менеджера → минимальная поверхность атаки
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
# сквозная самопроверка стека: docker compose exec api /selfcheck
COPY --from=build /out/selfcheck /selfcheck
COPY --from=build --chown=65532:65532 /out/data /data
ENV HTTP_ADDR=:8080 \
    DATA_FILE=/data/applications.json \
    USERS_FILE=/data/users.json \
    PIPELINE_STEP_DELAY=3s
EXPOSE 8080 9090
VOLUME ["/data"]
USER nonroot:nonroot
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD ["/api", "-healthcheck"]
ENTRYPOINT ["/api"]
