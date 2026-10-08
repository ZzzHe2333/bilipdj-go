FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
COPY internal ./internal
COPY web ./web
RUN mkdir -p /out/data && chown 65532:65532 /out/data && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/bilipdj-go .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/bilipdj-go /bilipdj-go
COPY --from=build --chown=65532:65532 /out/data /data
WORKDIR /data
VOLUME /data
EXPOSE 9816
USER 65532:65532
ENTRYPOINT ["/bilipdj-go","--listen","0.0.0.0:9816","--data","/data"]
