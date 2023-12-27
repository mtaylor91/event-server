FROM images.home.mtaylor.io/go:latest AS build
COPY . /build
WORKDIR /build
RUN go build

FROM images.home.mtaylor.io/base:latest AS run
COPY --from=build /build/event-server /usr/local/bin/event-server
ENTRYPOINT ["/usr/local/bin/event-server"]
