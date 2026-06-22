# mihomo-st CLI

## Usage

Start with built-in default config:

```sh
mihomo-st
```

Use an explicit listen address:

```sh
mihomo-st --listen 127.0.0.1:32198
```

Short flags:

```sh
mihomo-st -l 127.0.0.1:32198
```

## Flags

| Flag        | Short | Default           | Notes                                                                                                 |
| ----------- | ----- | ----------------- | ----------------------------------------------------------------------------------------------------- |
| `--listen`  | `-l`  | `127.0.0.1:32198` | REST server listen address. Must be explicit `host:port`, such as `127.0.0.1:32198` or `[::1]:32198`. |
| `--version` | `-v`  | `false`           | Print only the version number and exit.                                                               |

The process always starts with built-in default runtime config. Change runtime
config through the REST API `PATCH /config`; import proxy nodes through
`POST /proxies/import`.

Collection test requests can override runtime concurrency with positive
request-level `concurrency`. Omitted request `concurrency` falls back to runtime
config. Explicit `0` or negative request values return `400`. Single-proxy test
requests cannot set `concurrency`.

## Listen Address

`--listen` accepts explicit `host:port` values only.

Valid examples:

```text
127.0.0.1:32198
0.0.0.0:32198
localhost:32198
[::1]:32198
[::]:32198
```

Invalid examples:

```text
32198
:32198
127.0.0.1
::1:32198
http://127.0.0.1:32198
```

## Version

```sh
mihomo-st --version
mihomo-st -v
```

Expected output:

```text
<version>
```

The version command prints only the version number, with no prefix or extra
text.
