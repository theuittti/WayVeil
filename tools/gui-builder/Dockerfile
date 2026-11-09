# Builder for the Wayveil desktop app: Go + GTK3/WebKitGTK 4.1 headers + Node + Xvfb (for app-smoke).
FROM golang:1.27-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends \
      libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config xvfb xauth ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=node:22-bookworm /usr/local/bin/node /usr/local/bin/node
COPY --from=node:22-bookworm /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/npm
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx
