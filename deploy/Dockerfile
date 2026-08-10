FROM node:22-bookworm AS ui
WORKDIR /ui
COPY web/ui/package.json web/ui/package-lock.json* ./
RUN npm install
COPY web/ui/ ./
RUN npm run build

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /ui/dist ./web/ui/dist
RUN CGO_ENABLED=0 go build -o /knowledge-core ./cmd/knowledge-core

# Writable data dir for bootstrap password (uid 65532 = distroless nonroot).
FROM debian:bookworm-slim AS data
RUN mkdir -p /data && chown 65532:65532 /data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /knowledge-core /knowledge-core
COPY --from=data --chown=65532:65532 /data /data
COPY migrations /migrations
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/knowledge-core", "-migrations", "/migrations"]
