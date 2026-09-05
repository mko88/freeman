# Builds cmd/freeman-server — the headless HTTP/container counterpart to
# the desktop Wails app (cmd/freeman). This image is entirely independent
# of .devcontainer's heavier GUI toolchain: freeman-server has no Wails
# dependency, so no CGO/mingw/GTK/WebKit is needed anywhere in this build.

FROM node:22-bookworm AS frontend
WORKDIR /src
COPY cmd/freeman/frontend/package.json cmd/freeman/frontend/package-lock.json ./
RUN npm ci
COPY cmd/freeman/frontend .
RUN npm run build:web

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN rm -rf cmd/freeman-server/web && mkdir cmd/freeman-server/web
COPY --from=frontend /src/dist-web/. cmd/freeman-server/web/
RUN CGO_ENABLED=0 go build -o /out/freeman-server ./cmd/freeman-server

# distroless/static (not bare scratch) so httpengine.Execute can still
# verify TLS certs when a saved request targets an https:// API.
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/freeman-server /freeman-server
# All interfaces, unlike the binary's own 127.0.0.1 default: a published
# Docker port forwards to the container's IP, so a loopback-bound process
# inside would be unreachable. The container boundary is what keeps this
# private — docker-compose publishes it to the host's loopback only, and
# the server still has no auth of its own.
ENV FREEMAN_LISTEN=:8080
EXPOSE 8080
ENTRYPOINT ["/freeman-server"]
