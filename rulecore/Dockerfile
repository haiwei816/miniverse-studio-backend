FROM golang:1.27.3 AS build
RUN apt-get update && apt-get install -y --no-install-recommends cmake g++ libgrpc++-dev libprotobuf-dev protobuf-compiler protobuf-compiler-grpc && rm -rf /var/lib/apt/lists/*
RUN --mount=type=cache,target=/go/pkg/mod go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10 && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
WORKDIR /src
COPY proto ./proto
COPY rulecore ./rulecore
RUN cmake -S rulecore -B /build -DCMAKE_BUILD_TYPE=Release && cmake --build /build -j4 && ctest --test-dir /build --output-on-failure
RUN mkdir -p /runtime-libs && ldd /build/rulecore /build/rule_tests | awk '/=> \// {print $3}' | grep -Ev '/(libc|libm|libpthread|libdl|librt|ld-linux)\.so' | sort -u | xargs -I{} cp -L {} /runtime-libs/

FROM debian:trixie-slim AS runtime
COPY --from=build /runtime-libs/ /usr/local/lib/rulecore/
COPY --from=build /build/rulecore /build/rulecore
COPY --from=build /build/rule_tests /build/rule_tests
RUN echo "/usr/local/lib/rulecore" > /etc/ld.so.conf.d/rulecore.conf && ldconfig && /build/rule_tests
EXPOSE 50051
USER nobody
ENTRYPOINT ["/build/rulecore"]
