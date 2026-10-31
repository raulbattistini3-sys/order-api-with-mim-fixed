FROM alpine:3.20
ARG PB_VERSION=0.40.6
ARG TARGETARCH=amd64
RUN apk add --no-cache ca-certificates unzip wget \
 && wget -q "https://github.com/pocketbase/pocketbase/releases/download/v${PB_VERSION}/pocketbase_${PB_VERSION}_linux_${TARGETARCH}.zip" -O /tmp/pb.zip \
 && unzip /tmp/pb.zip pocketbase -d /pb \
 && rm /tmp/pb.zip
COPY pb_migrations /pb/pb_migrations
COPY entrypoint.sh /pb/entrypoint.sh
EXPOSE 8090
ENTRYPOINT ["/bin/sh", "/pb/entrypoint.sh"]
