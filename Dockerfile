# Build stage
FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/fast-crud ./cmd/api

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build --chown=nonroot:nonroot /out/fast-crud /app/fast-crud
COPY --from=build --chown=nonroot:nonroot /src/web /app/web
USER nonroot:nonroot
EXPOSE 3000
ENTRYPOINT ["/app/fast-crud"]
