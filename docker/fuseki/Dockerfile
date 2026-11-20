FROM eclipse-temurin:21-jre-jammy

ARG JENA_VERSION=6.2.0
ARG JENA_SHA512=ba65f5867d2d4741b2ed9e2af5a0d4fbb447909894ab2a0c6bc4dac8997f4fe339c87b13c48d45d054977769f0f8bf763ea346b1f7792d5cdc458041bd43a132

RUN apt-get update \
    && apt-get install --no-install-recommends --yes ca-certificates curl tar \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 fuseki \
    && useradd --system --uid 10001 --gid fuseki --home-dir /fuseki \
       --no-create-home --shell /usr/sbin/nologin fuseki \
    && mkdir -p /opt/fuseki /fuseki/databases \
    && chown -R 10001:10001 /fuseki

RUN set -eux; \
    archive="apache-jena-fuseki-${JENA_VERSION}.tar.gz"; \
    curl --fail --location --silent --show-error \
      "https://dlcdn.apache.org/jena/binaries/${archive}" \
      --output "/tmp/${archive}"; \
    echo "${JENA_SHA512}  /tmp/${archive}" | sha512sum --check --status; \
    tar --extract --gzip --file "/tmp/${archive}" \
      --directory /opt/fuseki --strip-components=1; \
    rm "/tmp/${archive}"

COPY config.ttl /etc/fuseki/config.ttl

WORKDIR /fuseki
USER 10001:10001
EXPOSE 3030
ENTRYPOINT ["/opt/fuseki/fuseki-server"]
CMD ["--config", "/etc/fuseki/config.ttl"]
