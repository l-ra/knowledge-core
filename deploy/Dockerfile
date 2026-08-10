FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /knowledge-core ./cmd/knowledge-core

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /knowledge-core /knowledge-core
COPY migrations /migrations
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/knowledge-core", "-migrations", "/migrations"]
