# Build Stage
FROM --platform=$BUILDPLATFORM golang:1.17-alpine as builder
ARG TARGETOS TARGETARCH
LABEL REPO="https://github.com/keikoproj/lifecycle-manager"

WORKDIR /go/src/github.com/keikoproj/lifecycle-manager
COPY . .

RUN apk update && apk add --no-cache build-base make git ca-certificates && update-ca-certificates
RUN addgroup -g 10001 -S lifecycle-manager && \
    adduser --disabled-password \
		--gecos "" \
		--home "/nonexistent" \
		--shell "/sbin/nologin" \
		--no-create-home \
                -G lifecycle-manager \
		--uid 10001 \
		lifecycle-manager
ADD https://storage.googleapis.com/kubernetes-release/release/v1.18.14/bin/linux/amd64/kubectl /usr/local/bin/kubectl
RUN chmod 777 /usr/local/bin/kubectl
RUN make build

# Final Stage
FROM scratch

ARG GIT_COMMIT
ARG VERSION
LABEL REPO="https://github.com/keikoproj/lifecycle-manager"
LABEL GIT_COMMIT=$GIT_COMMIT
LABEL VERSION=$VERSION

COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/local/bin/kubectl /usr/local/bin/kubectl
COPY --from=builder /go/src/github.com/keikoproj/lifecycle-manager/bin/lifecycle-manager /bin/lifecycle-manager

USER lifecycle-manager

CMD ["/bin/lifecycle-manager", "--help"]
