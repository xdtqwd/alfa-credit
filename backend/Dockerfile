# ---------- build ----------
FROM golang:1.24-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY go.mod ./
RUN go mod download
COPY . .
RUN go test ./... \
 && go build -ldflags="-s -w" -o /out/api ./cmd/api \
 && mkdir -p /out/data

# ---------- runtime ----------
# distroless: нет shell и пакетного менеджера → минимальная поверхность атаки
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
COPY --from=build --chown=65532:65532 /out/data /data
ENV HTTP_ADDR=:8080 \
    DATA_FILE=/data/applications.json \
    USERS_FILE=/data/users.json \
    PIPELINE_STEP_DELAY=3s
EXPOSE 8080
VOLUME ["/data"]
USER nonroot:nonroot
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD ["/api", "-healthcheck"]
ENTRYPOINT ["/api"]
