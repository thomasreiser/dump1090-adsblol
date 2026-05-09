# Multi-stage build for cmd/server. The build stage uses the official
# Go toolchain image; the runtime stage is distroless static for a small,
# rootless final image with no shell.
FROM golang:1.26-alpine AS build

WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 50051 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
