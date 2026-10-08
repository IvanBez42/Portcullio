FROM node:lts-alpine@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1
RUN apk add --no-cache openssl && test -x /usr/bin/openssl
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts
COPY index.js auth.js state.js views.js agentClient.js generate-recovery-code.js loginThrottle.js csrf.js vaultId.js tls.js ./
COPY public ./public
EXPOSE 8080
CMD ["node", "index.js"]
