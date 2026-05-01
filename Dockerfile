# Build the starter-api binary as a single statically-linked artefact.
# Distroless base for the runtime image — no shell, no package manager,
# nothing to upgrade after the build.

FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$(echo ${TARGETARCH:-amd64}) \
    go build -trimpath -ldflags='-s -w' -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/server /server
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/server"]
