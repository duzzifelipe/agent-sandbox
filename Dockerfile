FROM ubuntu:24.04

ARG TZ=America/Sao_Paulo
ENV TZ="$TZ"
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      git \
      gnupg \
      sudo \
      zsh \
      less \
      jq \
      unzip \
      build-essential \
      tzdata \
    && ln -fs /usr/share/zoneinfo/$TZ /etc/localtime \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

# Reuse the ubuntu:24.04 default user (uid 1000, name "ubuntu") and give it sudo.
ARG USERNAME=ubuntu
RUN echo "$USERNAME ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/$USERNAME \
    && chmod 0440 /etc/sudoers.d/$USERNAME \
    && mkdir -p /workspace /home/$USERNAME/.claude \
    && chown -R $USERNAME:$USERNAME /workspace /home/$USERNAME/.claude

USER $USERNAME
WORKDIR /home/$USERNAME

ENV SHELL=/bin/zsh
ENV MISE_DATA_DIR=/home/$USERNAME/.local/share/mise
ENV PATH=/home/$USERNAME/.local/bin:/home/$USERNAME/.local/share/mise/shims:$PATH

# Install mise (https://mise.jdx.dev) via the official installer.
RUN curl -fsSL https://mise.run | sh

# Pre-install node (lts) globally via mise.
RUN mise use -g node@lts && mise install

# Install Claude Code via the official installer (https://claude.ai/install.sh).
RUN curl -fsSL https://claude.ai/install.sh | bash
ENV PATH=/home/$USERNAME/.local/bin:$PATH

# Activate mise in interactive shells.
RUN echo 'eval "$(/home/ubuntu/.local/bin/mise activate zsh)"' >> /home/$USERNAME/.zshrc \
    && echo 'eval "$(/home/ubuntu/.local/bin/mise activate bash)"' >> /home/$USERNAME/.bashrc

# Convenience alias for skipping permission prompts (safe inside this sandbox).
RUN echo 'alias yolo="claude --dangerously-skip-permissions"' >> /home/$USERNAME/.zshrc \
    && echo 'alias yolo="claude --dangerously-skip-permissions"' >> /home/$USERNAME/.bashrc

COPY --chown=$USERNAME:$USERNAME entrypoint.sh /usr/local/bin/entrypoint.sh
RUN sudo chmod +x /usr/local/bin/entrypoint.sh

WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["zsh"]
