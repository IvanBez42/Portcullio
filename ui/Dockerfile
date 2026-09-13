FROM node:20-alpine
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev
COPY index.js auth.js state.js views.js agentClient.js generate-recovery-code.js loginThrottle.js csrf.js vaultId.js ./
COPY public ./public
EXPOSE 8080
CMD ["node", "index.js"]
