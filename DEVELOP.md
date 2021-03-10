# Develop PrimeHub Controller

## Prerequisite

- Install Go ~> 1.12.5

- Install kubebuilder 2.2.0, see [kubebuilder installation](https://book.kubebuilder.io/quick-start.html#installation)

    * For Apple M1 Device, please replace the etcd binary after installing kubebuilder

        ```bash
        curl -L https://github.com/etcd-io/etcd/releases/download/v3.4.15/etcd-v3.4.15-darwin-amd64.zip | tar -xz -C /tmp/
        sudo mv etcd-v3.4.15-darwin-amd64/etcd /usr/local/kubebuilder/bin
        sudo chmod +x /usr/local/kubebuilder/bin/etcd
        ```

- Install PrimeHub, see [PrimeHub INSTALL.md](https://github.com/InfuseAI/primehub/blob/master/INSTALL.md)

## Configuration

- Copy `config.yaml.example` to `config.yaml`

- Modify config content according to your Primehub installation.

## Develop

- Scale PrimeHub-controller to 0 by running `kubectl scale deployment primehub-controller --replicas=0 `

- Run `make run` to run PrimeHub-controller.

## Test

- Run `make test` to run unit tests.
