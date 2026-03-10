FROM golang:alpine AS build
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o subscription-service ./cmd/subscription-service

FROM alpine:3.19
WORKDIR /app
COPY --from=build /app/subscription-service /usr/local/bin/subscription-service
EXPOSE 8080
ENV PORT=8080
ENTRYPOINT ["/usr/local/bin/subscription-service"]