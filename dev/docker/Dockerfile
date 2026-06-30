FROM debian:trixie-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    libbtrfs-dev \
    libgpgme-dev \
    pkg-config \
    build-essential \
    ca-certificates \
    curl \
    wget \
    kpartx \
    lvm2 \
    git \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL https://go.dev/dl/go1.26.4.linux-amd64.tar.gz \
    | tar -xz -C /usr/local \
    && ln -sf /usr/local/go/bin/* /usr/local/bin/

RUN curl -fsSL https://github.com/neovim/neovim/releases/download/v0.12.0/nvim-linux-x86_64.tar.gz \
    | tar -xz -C /usr/local --strip-components=1 \
    && ln -sf /usr/local/bin/nvim /usr/bin/nvim

RUN mkdir -p /data/disks /data/config /data/src

WORKDIR /data/src
