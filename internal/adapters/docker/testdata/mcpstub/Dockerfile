# A stand-in for a stdio MCP server image. The integration tests cross-compile main.go for the
# Docker host (CGO_ENABLED=0) into the build context as mcpstub, so the image needs no base and
# the build pulls nothing. No USER: like many server images it would run as root, which the
# sidecar replaces with nobody.
FROM scratch
COPY mcpstub /mcpstub
ENTRYPOINT ["/mcpstub"]
