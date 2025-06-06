# Copyright 2022 The Go Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

FROM --platform=linux/amd64 golang:1.21-alpine AS builder

WORKDIR /app
COPY . .

ENV GOARCH=amd64
ENV GOOS=linux
RUN go build -o perf-server ./perf/

FROM --platform=linux/amd64 alpine:3.19

WORKDIR /app
COPY --from=builder /app/perf-server .

EXPOSE 8080
ENTRYPOINT ["./perf-server"]
