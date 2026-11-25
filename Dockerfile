# Image ghcr.io/ealfarozi/ptb, built by GoReleaser (dockers_v2) from the
# release binaries. distroless/static has CA certificates and runs as nonroot.
# Use PTB_API_KEY inside containers (no browser, no keychain).
FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/ptb /usr/bin/ptb
ENV PTB_CONFIG_DIR=/tmp/ptb
ENTRYPOINT ["/usr/bin/ptb"]
CMD ["--help"]
