#!/bin/bash

set -e

IMAGE_NAME="${IMAGE_NAME:-discopanel}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
REGISTRY="${REGISTRY:-nickheyer}"

if [ -n "$REGISTRY" ]; then
    FULL_IMAGE_NAME="${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}"
else
    FULL_IMAGE_NAME="${IMAGE_NAME}:${IMAGE_TAG}"
fi

echo "Building ${FULL_IMAGE_NAME}..."

docker build \
    --build-arg BUILDKIT_INLINE_CACHE=1 \
    --cache-from="${FULL_IMAGE_NAME}" \
    -t "${FULL_IMAGE_NAME}" \
    -f Dockerfile \
    .

if [ "$1" = "--push" ] && [ -n "$REGISTRY" ]; then
    echo "Pushing ${FULL_IMAGE_NAME}..."
    docker push "${FULL_IMAGE_NAME}"
fi

echo "Build complete: ${FULL_IMAGE_NAME}"

if [ "$1" = "--run" ]; then
    echo "Starting container..."
    docker run -d \
        --name discopanel \
        --restart unless-stopped \
        -p 8080:8080 \
        -v /var/run/docker.sock:/var/run/docker.sock:ro \
        -v discopanel_data:/app/data \
        -v discopanel_backups:/app/backups \
        "${FULL_IMAGE_NAME}"
    echo "Container started as 'discopanel'"
fi

