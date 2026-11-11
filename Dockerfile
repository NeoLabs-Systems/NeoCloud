# syntax=docker/dockerfile:1

# NeoCloud server: Express API, the bundled web client and SQLite. Everything
# that changes at runtime -- database, encrypted files, .env with the master
# key -- lives in the /data volume; the image itself is never modified.

ARG NODE_IMAGE=node:22-trixie-slim

FROM ${NODE_IMAGE} AS deps
RUN apt-get update \
 && apt-get install -y --no-install-recommends python3 make g++ \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --no-audit --no-fund && npm cache clean --force

FROM ${NODE_IMAGE}
# Office previews need LibreOffice (~400 MB). Build with
# --build-arg WITH_LIBREOFFICE=true to include it.
ARG WITH_LIBREOFFICE=false
RUN apt-get update \
 && apt-get install -y --no-install-recommends tini ca-certificates \
 && if [ "$WITH_LIBREOFFICE" = "true" ]; then apt-get install -y --no-install-recommends libreoffice-core libreoffice-writer libreoffice-calc libreoffice-impress; fi \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY package.json package-lock.json LICENSE ./
COPY bin ./bin
COPY lib ./lib
COPY runtime ./runtime
COPY server ./server
COPY landing ./landing
RUN ln -s /app/bin/neocloud.js /usr/local/bin/neocloud \
 && mkdir /data \
 && chown node:node /data

ENV NODE_ENV=production \
    NEOCLOUD_HOME=/data \
    PORT=3344

USER node
VOLUME /data
EXPOSE 3344
# /api/health answers 503 when the database or the data volume is not usable.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:'+(process.env.PORT||3344)+'/api/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["node", "server/index.js"]
