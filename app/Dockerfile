# Сборка и запуск в двух стадиях: в финальный образ попадает только
# бинарник — без shell, curl и компилятора, то есть минимальная поверхность.
FROM golang:1.27-alpine AS build
WORKDIR /src

# Слой с зависимостями отдельный: когда они появятся, сюда добавится
# go mod download, и правки кода не будут заставлять качать их заново.
COPY go.mod ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w \
      -X main.version=${VERSION} \
      -X main.commit=${COMMIT} \
      -X main.buildTime=${BUILT_AT}" \
    -o /out/app .

# distroless: ни пакетного менеджера, ни libc, пользователь nonroot из коробки.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
USER nonroot:nonroot

ENV ADDR=:8080
EXPOSE 8080

# В distroless нет ни shell, ни curl, поэтому healthcheck выполняет сам бинарник.
HEALTHCHECK --interval=30s --timeout=5s --start-period=3s --retries=3 \
  CMD ["/app", "-healthcheck"]

ENTRYPOINT ["/app"]
