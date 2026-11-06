FROM golang:1.27.3 AS build
RUN apt-get update && apt-get install -y --no-install-recommends cmake g++ libgrpc++-dev libprotobuf-dev protobuf-compiler protobuf-compiler-grpc && rm -rf /var/lib/apt/lists/*
RUN --mount=type=cache,target=/go/pkg/mod go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10 && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
WORKDIR /src
COPY proto ./proto
COPY rulecore ./rulecore
RUN cmake -S rulecore -B /build -DCMAKE_BUILD_TYPE=Release && cmake --build /build -j4 && ctest --test-dir /build --output-on-failure
FROM build AS runtime
EXPOSE 50051
USER nobody
ENTRYPOINT ["/build/rulecore"]
