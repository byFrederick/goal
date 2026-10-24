# The egress proxy for the integration tests: cmd/goal-egress cross-compiled on the host and
# copied into an empty image, so the build pulls nothing. task egress:image builds the real one
# (cmd/goal-egress/Dockerfile).
FROM scratch
COPY goal-egress /goal-egress
ENTRYPOINT ["/goal-egress"]
