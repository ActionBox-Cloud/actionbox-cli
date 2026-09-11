# ActionBox CLI

Command-line client for the hosted [ActionBox](https://actionbox.cloud) API.
Use it to request human decisions from scripts and terminal workflows.

## Install

Install the CLI from PyPI with Python 3.9 or newer:

```sh
python -m pip install actionbox
```

## Get started

Create a Source in ActionBox, then configure the CLI interactively:

```sh
actionbox configure
actionbox doctor
actionbox ask "Proceed with the operation?" --option approve --option reject --wait
```

The client uses `https://api.actionbox.cloud`. Keep Source keys out of source
control and logs.

## Development

Use the Go version declared in `go.mod` or newer:

```sh
go test ./...
go test -race ./...
go vet ./...
go build -trimpath -o actionbox ./cmd/actionbox
./actionbox --help
```

The `actionbox_pkg` directory contains the Python launcher used to distribute
the Go executable. Local Go development does not require that launcher.

## Documentation and contributions

- [ActionBox documentation](https://actionbox.cloud/docs)
- [Contributing](CONTRIBUTING.md)
- [Report a vulnerability](SECURITY.md)

## License

This client is MIT licensed; see [LICENSE](LICENSE).
ActionBox is proprietary hosted software. This repository contains its client.
