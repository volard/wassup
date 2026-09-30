FROM ghcr.io/charmbracelet/vhs:latest

RUN apt-get update \
    && apt-get install -y --no-install-recommends neovim \
    && rm -rf /var/lib/apt/lists/*
