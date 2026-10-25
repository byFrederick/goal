# A stand-in for goald and the auth service in the topology check (topology.sh): on 8080, 8081
# and 8090 it answers every request with its port, the request and the names (never the values)
# of its environment.
FROM node:22-bookworm-slim@sha256:de170b39439eb0e5a3444ad7d60b015614a381e10fecae2e30ec9e735122f38d
COPY echo.mjs /echo.mjs
USER 1000:1000
ENTRYPOINT ["node", "/echo.mjs"]
