FROM node:lts-alpine@sha256:c6ad9e9c1d80f6eb607c26b4b0a132ff7a2344fad1c78ea3baa2a7d4c8de6d94
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
