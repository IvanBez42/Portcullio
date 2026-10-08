FROM node:26-alpine
RUN apk add --no-cache openssl && test -x /usr/bin/openssl
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts
COPY index.js auth.js state.js views.js agentClient.js generate-recovery-code.js loginThrottle.js csrf.js vaultId.js tls.js ./
COPY public ./public
EXPOSE 8080
CMD ["node", "index.js"]
