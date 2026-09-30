# Two stages so the shipped image has no npm, no shell and no build
# tools. distroless/nodejs24-debian12:nonroot already runs as uid 65532.
#
# Neither stage is pinned by digest: a floating tag lands base changes
# on the next build. Pin both to a digest if that matters more than
# picking up patch releases.
FROM node:24-alpine AS build

WORKDIR /repo

# The REPOSITORY root is the build context, not this service's
# directory - that is what lets a service depend on a generated client
# in a sibling directory (`file:../<api>/clients/ts`).
COPY . .

WORKDIR /repo/services/pokedex-htmx

# Dev deps included: the bundler and TypeScript are devDependencies and
# neither ends up in the image.
RUN npm ci

# One file out. Vite inlines every dependency (ssr.noExternal), so the
# runtime stage needs no node_modules and the sibling client's symlink
# never has to survive into the container.
RUN npm run build

FROM gcr.io/distroless/nodejs24-debian12:nonroot

# Stamped so every log line says which build produced it.
ARG VERSION=dev
ENV VERSION=$VERSION

WORKDIR /app
COPY --from=build --chown=65532:65532 /repo/services/pokedex-htmx/dist/server.js ./

USER 65532:65532
EXPOSE 3000
# The distroless nodejs image's entrypoint is already `node`.
CMD ["server.js"]
