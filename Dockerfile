# Delivery vehicle for the Coraza WAF wasm plugin as a container image — not a
# running service. Mirrors the release zip's contents (.traefik.yml, the wasm,
# LICENSE) so a consumer that wants an image — e.g. a Kubernetes initContainer
# copying the plugin into Traefik's local-plugin directory — can pull one
# instead of downloading and unzipping the release asset themselves.
#
# Built by .github/workflows/ci.yaml on tag push, from the same wasm that
# workflow already attaches to the GitHub Release, and pushed to ghcr.io.
FROM busybox:1.37

# Copied individually and 0644 root-owned (i.e. world-readable): a
# `COPY *` glob would silently skip .traefik.yml, since shell globs don't
# match dotfiles, and Traefik reads wasmPath from that manifest.
COPY .traefik.yml /plugin/.traefik.yml
COPY build/coraza-http-wasm.wasm /plugin/coraza-http-wasm.wasm
COPY LICENSE /plugin/LICENSE
