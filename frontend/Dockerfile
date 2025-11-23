# syntax=docker/dockerfile:1
FROM node:20-alpine as builder
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
ARG VITE_APP_API_BASE=http://localhost:8080
ENV VITE_APP_API_BASE=$VITE_APP_API_BASE
RUN npm run build

FROM node:20-alpine
WORKDIR /app
COPY --from=builder /app /app
EXPOSE 4173
CMD ["npm", "run", "preview", "--", "--host", "--port", "4173"]
