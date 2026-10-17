FROM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/vindice ./cmd/vindice

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/vindice /vindice
EXPOSE 4317 4318 8080
ENTRYPOINT ["/vindice"]
