# Pinned by digest: typedb/typedb:3.14.2 (published 2026-11-04).
FROM typedb/typedb:3.14.2@sha256:2de1c146d7eacc3b7c0a61cd4b9f7bf5c69d7b00164d6f2e885e2f8573830454

# The upstream image runs as root and ships its executables as 0744. Make
# them executable for an unprivileged user and give that user the data
# directory; the named volume inherits this ownership on first use.
RUN chmod 0755 /opt/typedb-server-linux-x86_64/typedb \
        /opt/typedb-server-linux-x86_64/server/typedb_server_bin \
    && mkdir -p /var/lib/typedb/data /var/lib/typedb/logs \
    && chown -R 10001:10001 /var/lib/typedb
USER 10001:10001
