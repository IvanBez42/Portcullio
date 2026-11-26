FROM node:lts-alpine@sha256:6bd9ab43c0ec0190ca7b2d193906cb8b7edf6e7f4afa43ddeccd6452710bffb5
RUN apk add --no-cache openssl && test -x /usr/bin/openssl
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts
ARG PORTCULLIO_VERSION=unknown
ENV PORTCULLIO_VERSION=$PORTCULLIO_VERSION
COPY index.js auth.js state.js views.js agentClient.js generate-recovery-code.js loginThrottle.js csrf.js vaultId.js tls.js ./
COPY public ./public
EXPOSE 8080
CMD ["node", "index.js"]
