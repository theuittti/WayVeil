# Integration-test client machine: an ordinary user with a TUN-capable mihomo.
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
        curl ca-certificates iproute2 tcpdump libcap2-bin procps \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -m -s /bin/bash app
CMD ["sleep", "infinity"]
