FROM catthehacker/ubuntu:act-24.04
USER root
RUN apt-get update && apt-get install -y --no-install-recommends openjdk-21-jdk-headless git curl ca-certificates build-essential pkg-config && rm -rf /var/lib/apt/lists/*
ENV JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64 RUSTUP_HOME=/opt/rustup CARGO_HOME=/opt/cargo PATH=/opt/cargo/bin:$PATH
RUN curl -sSf https://sh.rustup.rs | sh -s -- -y --profile minimal -c rustfmt -c clippy && chmod -R a+rwX /opt/rustup /opt/cargo
RUN git config --system --add safe.directory '*'
