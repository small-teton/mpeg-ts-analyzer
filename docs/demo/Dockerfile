FROM golang:1.27.4-alpine3.24@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf

RUN apk add --no-cache \
    bash \
    font-dejavu \
    imagemagick

WORKDIR /workspace

ENTRYPOINT ["./docs/demo/render.sh"]
