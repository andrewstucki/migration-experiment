FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY ./migration-operator .

USER nonroot:nonroot

ENTRYPOINT ["/migration-operator"]