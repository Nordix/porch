# Copyright 2022-2025 The kpt and Nephio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM ghcr.io/kptdev/krm-functions-catalog/apply-setters:latest AS apply-setters
FROM ghcr.io/kptdev/krm-functions-catalog/apply-setters:latest AS apply-setters-v1
FROM ghcr.io/kptdev/krm-functions-catalog/ensure-name-substring:latest AS ensure-name-substring
FROM ghcr.io/kptdev/krm-functions-catalog/search-replace:latest AS search-replace
FROM ghcr.io/kptdev/krm-functions-catalog/set-annotations:latest AS set-annotations
FROM ghcr.io/kptdev/krm-functions-catalog/set-image:latest AS set-image
FROM ghcr.io/kptdev/krm-functions-catalog/set-labels:latest AS set-labels
FROM ghcr.io/kptdev/krm-functions-catalog/set-namespace:latest AS set-namespace
FROM ghcr.io/kptdev/krm-functions-catalog/set-project-id:latest AS set-project-id
FROM ghcr.io/kptdev/krm-functions-catalog/starlark:latest AS starlark
FROM ghcr.io/kptdev/krm-functions-catalog/upsert-resource:latest AS upsert-resource

FROM golang:1.25.1-alpine3.21 AS builder
WORKDIR /go/src

RUN go install github.com/grpc-ecosystem/grpc-health-probe@v0.4.11
RUN cp $GOPATH/bin/grpc-health-probe /grpc-health-probe

COPY . .
RUN cd func ; go build -v -o /server ./server
COPY func/config.yaml /config.yaml

FROM alpine:3.21

COPY --from=apply-setters          /usr/local/bin/function /functions/apply-setters
COPY --from=apply-setters-v1       /usr/local/bin/function /functions/apply-setters-v1
COPY --from=ensure-name-substring  /usr/local/bin/function /functions/ensure-name-substring
COPY --from=search-replace         /usr/local/bin/function /functions/search-replace
COPY --from=set-annotations        /usr/local/bin/function /functions/set-annotations
COPY --from=set-image              /usr/local/bin/function /functions/set-image
COPY --from=set-labels             /usr/local/bin/function /functions/set-labels
COPY --from=set-namespace          /usr/local/bin/function /functions/set-namespace
COPY --from=set-project-id         /usr/local/bin/function /functions/set-project-id
COPY --from=starlark               /usr/local/bin/function /functions/starlark
COPY --from=upsert-resource        /usr/local/bin/function /functions/upsert-resource
COPY --from=builder                /server /grpc-health-probe /config.yaml /

EXPOSE 9445/tcp
ENTRYPOINT [ "/server", "--config=/config.yaml", "--functions=/functions" ]
