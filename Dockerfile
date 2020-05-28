# Create build stage and build the binaries
FROM golang:1.13.4-buster

WORKDIR $GOPATH/src/github.com/ava-labs/gecko
COPY . .
RUN ./scripts/build.sh

# Create final image with binaries and no build dependencies
FROM debian:buster

EXPOSE 9650
EXPOSE 9651
WORKDIR /gecko
VOLUME /var/lib/avalanche
ENTRYPOINT ["./build/avalanche", "-db-dir", "/var/lib/avalanche"]

# Copy binaries from build stage
COPY --from=0 /go/src/github.com/ava-labs/gecko /gecko
