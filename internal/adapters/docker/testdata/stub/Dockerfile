# A stand-in for the worker image: it speaks the worker's stdout protocol from a script and never
# calls a model. The integration tests build it as goal-runner-stub:test.
FROM busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e
COPY stub.sh /stub.sh
USER 1000:1000
ENTRYPOINT ["/bin/sh", "/stub.sh"]
