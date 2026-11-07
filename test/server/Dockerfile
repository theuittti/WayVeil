# Integration-test "VPS": Ubuntu 24.04 with systemd and sshd.
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
        systemd systemd-sysv openssh-server sudo python3 curl ca-certificates iproute2 logrotate \
    && rm -rf /var/lib/apt/lists/* \
    && echo 'root:wayveil' | chpasswd \
    && sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin yes/; s/^#\?PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config \
    && useradd -m -s /bin/bash deploy && echo 'deploy:deploy' | chpasswd \
    && echo 'deploy ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/deploy && chmod 0440 /etc/sudoers.d/deploy \
    && useradd -m -s /bin/bash -G sudo nopw && echo 'nopw:nopw' | chpasswd \
    && systemctl enable ssh
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
