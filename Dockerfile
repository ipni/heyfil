FROM golang:1.20-bullseye as build

WORKDIR /go/src/heyfil

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /go/bin/heyfil

FROM gcr.io/distroless/static-debian11
COPY --from=build /go/bin/heyfil /usr/bin/

ENTRYPOINT ["/usr/bin/heyfil"]
