![logo](https://github.com/InfuseAI/primehub/raw/master/docs/media/logo.png?raw=true "PrimeHub")

[![CircleCI](https://circleci.com/gh/InfuseAI/primehub.svg?style=svg)](https://circleci.com/gh/InfuseAI/primehub-controller)
[![ee docker](https://img.shields.io/docker/pulls/infuseai/primehub-controller-ee?label=ee%20docker%20pulls)](https://hub.docker.com/r/infuseai/primehub-controller-ee)
[![ce docker](https://img.shields.io/docker/pulls/infuseai/primehub-controller?label=ce%20docker%20pulls)](https://hub.docker.com/r/infuseai/primehub-controller)
[![codecov](https://codecov.io/gh/InfuseAI/primehub-controller/branch/master/graph/badge.svg?token=42RPVTS5L8)](https://codecov.io/gh/InfuseAI/primehub-controller)

# PrimeHub Controller

## Custom Image

### Config Map

1. Add build-and-push script to ConfigMap

    ```
    kubectl -n hub create configmap primehub-controller-custom-image-scripts --from-file ./scripts/custom_image/
    ```

### Push Secret

1. Create push secret for container registry, all built images will be pushed using this secret.

    ```
    kubectl -n hub create secret docker-registry $PUSH_SECRET_NAME --docker-server=$DOCKER_SERVER --docker-username=$DOCKER_USERNAME --docker-password="$DOCKER_PASSWORD"
    ```

### Configuration

1. Copy `config.yaml.example` to `config.yaml`
2. Edit `config.yaml`
