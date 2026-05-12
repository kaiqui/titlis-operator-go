FROM golang:1.24-alpine AS builder

WORKDIR /build
COPY src/go.mod src/go.sum ./src/
RUN cd src && go mod download

COPY src/ ./src/
RUN cd src && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -buildvcs=false -ldflags="-s -w" -o /bin/titlis-operator      ./cmd/operator && \
    go build -buildvcs=false -ldflags="-s -w" -o /bin/castai-monitor       ./cmd/castai-monitor && \
    go build -buildvcs=false -ldflags="-s -w" -o /bin/synthetic-monitor    ./cmd/synthetic-monitor

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /bin/titlis-operator   /titlis-operator
COPY --from=builder /bin/castai-monitor    /castai-monitor
COPY --from=builder /bin/synthetic-monitor /synthetic-monitor
COPY src/config/ /config/

USER nonroot:nonroot

# Default: operator principal. Outros charts sobrescrevem com:
#   command: ["/castai-monitor"] ou command: ["/synthetic-monitor"]
ENTRYPOINT ["/titlis-operator"]
