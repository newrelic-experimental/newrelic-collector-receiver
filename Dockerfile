FROM golang:1.18-alpine as builder

WORKDIR /build

# Let's cache modules retrieval - those don't change so often
COPY go.mod go.sum ./
RUN go mod download -x

# Copy the code necessary to build the application
# You may want to change this to copy only what you actually need.
COPY ./cmd/ ./cmd
COPY ./internal/ ./internal

# Build the application
# The GIT hash is optionally passed in via docker build arg and used to set version.GitHash in the binary build
# This allows the git hash to be recorded via user agent string for NR debug purposes
ARG GIT_HASH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -v -a \
    -ldflags "-X me.localhost/newrelic-ot-collector/internal/version.Version=$GIT_HASH -s -w" -tags 'osusergo netgo' \
    ./cmd/otelcol-nr

# Enable Go's DNS resolver to read from /etc/hosts
RUN echo "hosts: files dns" > /etc/nsswitch.conf.min

# Create a minimal passwd so we can run as non-root in the container
RUN echo "nobody:x:65534:65534:Nobody:/:" > /etc/passwd.min

# Let's create a /dist folder containing just the files necessary for runtime.
# Later, it will be copied as the / (root) of the output image.
WORKDIR /dist
RUN cp /build/otelcol-nr ./otelcol-nr
COPY otel-config.yaml ./
COPY ./cmd/otelcol-nr/LocalOTelCollectorTesting.* ./

# Create the minimal runtime image
FROM scratch

#COPY --chown=0:0 --from=builder /etc/nsswitch.conf.min /etc/nsswitch.conf
#COPY --chown=0:0 --from=builder /etc/passwd.min /etc/passwd
#COPY --chown=0:0 --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
#COPY --chown=0:0 --from=builder /dist /

COPY --from=builder /etc/nsswitch.conf.min /etc/nsswitch.conf
COPY --from=builder /etc/passwd.min /etc/passwd
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /dist /

#USER nobody

ENTRYPOINT ["/otelcol-nr"]
CMD ["--config", "/otel-config.yaml"]
EXPOSE 1777 55679 4317 9411
