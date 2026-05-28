# --- build stage ------------------------------------------------------------
FROM golang:1.22-alpine AS builder

WORKDIR /src

# Step 1: pull modules using only go.mod. `go mod download` populates go.sum
# in the image so the repo can be shipped without a checked-in go.sum.
# We avoid `go mod tidy` on purpose: it walks every test-only transitive
# dependency, some of which require a newer toolchain than this base image.
COPY go.mod ./
RUN go mod download

# Step 2: copy source (won't overwrite the generated go.sum because the
# source tree has none) and build the API binary.
COPY . .

# Step 3: static binary, no cgo — copies cleanly into a minimal runtime image.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -mod=mod \
    -o /out/api \
    ./cmd/api

# --- runtime stage ----------------------------------------------------------
FROM alpine:3.19

# ca-certificates for outbound TLS; tzdata so timestamps render nicely.
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /out/api /app/api
COPY migrations /app/migrations

# Drop privileges.
RUN addgroup -S app && adduser -S app -G app
USER app

EXPOSE 8080
ENTRYPOINT ["/app/api"]
