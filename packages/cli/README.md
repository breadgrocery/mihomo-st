# mihomo-st

A small Go REST server for testing [mihomo](https://github.com/MetaCubeX/mihomo) proxies.

It accepts a mihomo-compatible YAML config and only reads `proxies`. It is not a full mihomo runtime: it does not create proxy groups, expose a proxy listener, hijack DNS, or load rules.

## Usage

Run with built-in defaults:

```sh
mihomo-st
```

- The server listens on `127.0.0.1:32198` by default.

- Import proxies through the REST API, and change runtime config through `PATCH /config`.

- See [CLI usage](docs/CLI.md) and [REST API](docs/API.md).
