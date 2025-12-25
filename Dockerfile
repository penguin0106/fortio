# Build the binaries in larger image
# First build the build image: docker build -t fortio-build -f Dockerfile.build .
FROM fortio-build AS build
WORKDIR /build
COPY --chown=build:build . fortio
# Use build mode instead of install to avoid git version requirement
RUN make -C fortio official-build BUILD_DIR=/build MODE=build OFFICIAL_BIN=/build/result/fortio

# Minimal image with just the binary and certs
FROM scratch AS release
# We don't need to copy certs anymore since cli 1.6.0
# COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /build/result/fortio /usr/bin/fortio
EXPOSE 8078
EXPOSE 8079
EXPOSE 8080
EXPOSE 8081
# configmap (dynamic flags)
VOLUME /etc/fortio
# data files etc
VOLUME /var/lib/fortio
WORKDIR /var/lib/fortio
ENTRYPOINT ["/usr/bin/fortio"]
# start the server mode (grpc ping on 8079, http echo and UI on 8080, redirector on 8081) by default
CMD ["server", "-config-dir", "/etc/fortio"]

