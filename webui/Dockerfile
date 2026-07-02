# Stage 1: Build
FROM oven/bun:1.3.14-alpine AS build
WORKDIR /app
COPY package.json ./
RUN bun install
COPY . .
ARG VERSION
ENV VERSION=${VERSION}
RUN bun run build

# Stage 2: Serve
FROM nginx:alpine
COPY --from=build /app/dist/index.html /usr/share/nginx/html/index.html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 3000
CMD ["nginx", "-g", "daemon off;"]
