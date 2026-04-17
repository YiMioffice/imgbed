FROM node:20-alpine AS frontend-build
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.24-alpine AS backend-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/machring ./cmd/machring

FROM alpine:3.22 AS backend
WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata
COPY --from=backend-build /out/machring /usr/local/bin/machring
VOLUME ["/var/lib/machring"]
ENV MACHRING_HTTP_ADDR=:8080
ENV MACHRING_DATA_DIR=/var/lib/machring
ENV MACHRING_DATABASE_PATH=/var/lib/machring/machring.db
ENV MACHRING_UPLOAD_DIR=/var/lib/machring/uploads
ENV MACHRING_TEMP_DIR=/var/lib/machring/tmp
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=5 CMD wget -qO- http://127.0.0.1:8080/api/health >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/machring"]

FROM nginx:1.27-alpine AS frontend
COPY docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=frontend-build /src/frontend/dist /usr/share/nginx/html
EXPOSE 80
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=5 CMD wget -qO- http://127.0.0.1/ >/dev/null || exit 1
