ARG RUNTIME_BASE=debian:bookworm-slim
FROM ${RUNTIME_BASE}

ARG BINARY_PATH=dist/release/linux-amd64/tinycdn
ARG WEB_PATH=dist/release/web

WORKDIR /app

RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system tinycdn \
    && useradd --system --gid tinycdn --create-home --home-dir /app tinycdn \
    && mkdir -p /app/data/cache/badger /app/web \
    && chown -R tinycdn:tinycdn /app

COPY ${BINARY_PATH} /app/tinycdn
COPY ${WEB_PATH}/ /app/web/dist/
RUN chmod 0755 /app/tinycdn && chown -R tinycdn:tinycdn /app

USER tinycdn

EXPOSE 8787 8080
VOLUME ["/app/data"]

ENTRYPOINT ["/app/tinycdn"]
