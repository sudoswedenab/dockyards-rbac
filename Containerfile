FROM docker.io/library/golang:1.25.0 AS builder
COPY . /src
WORKDIR /src
ENV CGO_ENABLED=0
RUN go build -o dockyards-rbac -ldflags="-s -w"

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /src/dockyards-rbac /usr/bin/dockyards-rbac
ENTRYPOINT ["/usr/bin/dockyards-rbac"]
