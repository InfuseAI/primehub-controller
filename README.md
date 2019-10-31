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
