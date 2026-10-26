# sandboxtest's box: busybox's sh and curl (the docker adapter's suite image), with GNU coreutils
# for timeout, which the Worker runs every command under.
FROM curlimages/curl:8.17.0@sha256:935d9100e9ba842cdb060de42472c7ca90cfe9a7c96e4dacb55e79e560b3ff40
USER root
RUN apk add --no-cache coreutils
USER 100:101
WORKDIR /home/curl_user
