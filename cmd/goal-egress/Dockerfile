# syntax=docker/dockerfile:1
# Goal's egress proxy (cmd/goal-egress, ADR 0009). Build from the repository root (task egress:image):
#   docker build -f cmd/goal-egress/Dockerfile -t ghcr.io/byfrederick/goal-egress:0.1.0 .
# goald's Docker adapter runs it on the agents' and tools' networks; it is never pushed from here.

ARG GO_IMAGE=golang:1.26.10-alpine@sha256:27f2d3b83cfc5803c8078f8e730af0f7c431bf067afaec8f60a36fb763187cba
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY internal/egress/ internal/egress/
COPY cmd/goal-egress/ cmd/goal-egress/
# The proxy imports the standard library only, so nothing is downloaded.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/goal-egress ./cmd/goal-egress

# Distroless static: CA certificates (for an https gateway), a nonroot user, no shell.
FROM ${RUNTIME_IMAGE} AS runtime
COPY --from=build /out/goal-egress /goal-egress
USER 65532:65532
EXPOSE 3128
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --start-interval=1s CMD ["/goal-egress", "-health"]
ENTRYPOINT ["/goal-egress"]
