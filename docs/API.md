# mihomo-st API

Default base URL:

```text
http://127.0.0.1:32198
```

All request and response bodies are JSON unless noted otherwise. The server only
loads and tests top-level mihomo `proxies`.

Public names use kebab-case.

## Request Decoding

Request bodies are decoded strictly. Unknown fields, `null`, malformed JSON, and
extra JSON values return `400`.

For positive numeric request fields, omit the field to use fallback behavior.
Explicit `0` or negative values return `400`. This applies when an endpoint
accepts fields such as `timeout`, `rounds`, `max-bytes`, `concurrency`, or
`proxy-server.timeout`.

## Responses

Successful responses are endpoint-specific JSON objects. There is no global
success envelope.

Errors use the HTTP status code and this body:

```json
{
  "error": {
    "code": 400,
    "status": "BAD_REQUEST",
    "message": "payload is required"
  }
}
```

`GET /` is not implemented.

## Version

```http
GET /version
```

```json
{
  "name": "mihomo-st",
  "version": "<version>"
}
```

`version` is the current program version.

## Digest

```http
POST /digest
Content-Type: application/json
```

The body must be a JSON object. The response is:

```json
{
  "digest": "a1b2c3d4e5f60718_9a0b1c2d3e4f5678"
}
```

The digest is `canonical_endpoint`. The canonical part excludes top-level
`name`, `metadata`, and fields whose names start with `_`. Nested fields are
kept. The endpoint part uses the top-level `type/server/port` tuple, with
missing values treated as `undefined`.

## Config

```http
GET /config
PATCH /config
```

Default config:

```json
{
  "default-timeout": 5000,
  "proxy-server": {
    "expand": false,
    "nameservers": ["system"],
    "timeout": 5000
  },
  "delay": {
    "urls": [{ "url": "https://www.google.com/generate_204" }],
    "timeout": 10000,
    "follow-redirect": true,
    "expected": "200-299",
    "rounds": 2,
    "concurrency": 100,
    "unified": true
  },
  "download": {
    "urls": [{ "url": "https://cachefly.cachefly.net/50mb.test" }],
    "timeout": 10000,
    "follow-redirect": true,
    "rounds": 1,
    "max-bytes": 104857600,
    "concurrency": 1
  }
}
```

`PATCH /config` deep merges objects, replaces arrays, rejects unknown fields and
`null`, validates the merged config, and leaves the current config unchanged on
failure. Omitted fields leave existing values unchanged. Explicit `0` or
negative values for positive config fields return `400`. Changing config does
not rebuild the proxy snapshot.

`delay` and `download` support shared HTTP fields at the root:
`headers`, `timeout`, and `follow-redirect`. `headers` is an object of string
HTTP header names to string values. `timeout` is milliseconds.
`follow-redirect` defaults to `true`.

`delay.urls` items may be strings or objects with `url`, `headers`, `timeout`,
`follow-redirect`, `expected`, `rounds`, and `unified`. `download.urls` items
may be strings or objects with `url`, `headers`, `timeout`, `follow-redirect`,
`rounds`, and `max-bytes`. In API config patches, omit URL item numeric
overrides to use fallback values; explicit `0` or negative values are invalid.

## Proxies

### Import

```http
POST /proxies
Content-Type: application/json
```

Request:

```json
{
  "type": "local",
  "payload": "configs/nodes.yaml",
  "mode": "replace",
  "proxy-server": {
    "expand": true,
    "nameservers": ["system"],
    "timeout": 5000
  }
}
```

Fields:

- `type`: `text`, `local`, or `remote`.
- `payload`: config text, a local path, or an HTTP(S) URL.
- `mode`: `replace` or `append`; defaults to `replace`.
- `headers`: optional object of string HTTP headers. It is used only for
  `remote` imports; `text` and `local` imports ignore it.
- `timeout`: optional positive remote import timeout in milliseconds. Omit it
  to use `default-timeout`; explicit `0` or negative values return `400`.
- `follow-redirect`: optional remote import redirect behavior. Omit it to use
  `true`.
- `proxy-server`: optional import-time expansion override. If omitted, the
  current runtime `proxy-server` config is used. If supplied, the override
  starts from built-in `proxy-server` defaults and applies the provided fields.
  `proxy-server.timeout` must be positive when present; omit it to use the
  built-in proxy-server timeout for that override.

Remote import with custom HTTP fields:

```json
{
  "type": "remote",
  "payload": "https://example.com/nodes.yaml",
  "headers": {
    "Authorization": "Bearer token",
    "User-Agent": "custom-agent"
  },
  "timeout": 5000,
  "follow-redirect": true
}
```

Remote fetch headers are merged case-insensitively with internal defaults.
`User-Agent: clash.meta` is applied when no higher-precedence `User-Agent` is
provided.

Relative local paths resolve against the executable directory. Remote bodies are
limited to 100 MiB. Remote non-2xx responses return `502`, timeouts return
`504`, and body limit failures return `413`.

After source text is read, Base64 decoding is attempted. If decoding succeeds,
the decoded text is parsed as YAML; otherwise the original text is parsed.

Only top-level `proxies` is read. Whole YAML parse failure fails the request.
Invalid individual proxy entries are returned as warnings and skipped.
Successful imports always publish a new snapshot and increment the version,
including imports with zero proxies.

Response:

```json
{
  "version": 4,
  "proxies": [
    {
      "digest": "canonical_endpoint",
      "type": "ss",
      "name": "node-a",
      "server": "1.2.3.4",
      "port": 8388
    }
  ],
  "warnings": []
}
```

### List

```http
GET /proxies
```

```json
{
  "version": 4,
  "proxies": [
    {
      "digest": "canonical_endpoint",
      "type": "ss",
      "name": "node-a",
      "server": "1.2.3.4",
      "port": 8388
    }
  ]
}
```

`GET /proxies` never expands or mutates inventory.

### One-shot proxy request

```http
POST /proxies/{digest}/proxy
Content-Type: application/json
```

Request:

```json
{
  "url": "https://httpbin.org/post",
  "method": "POST",
  "headers": {
    "Content-Type": "application/json"
  },
  "timeout": 10000,
  "follow-redirect": true,
  "body": "{\"hello\":\"world\"}"
}
```

Fields:

- `url`: required HTTP or HTTPS URL.
- `method`: optional HTTP method. It defaults to `GET`.
- `headers`: optional object of string HTTP headers.
- `timeout`: optional positive timeout in milliseconds. Omit it to use
  `default-timeout`; explicit `0` or negative values return `400`.
- `follow-redirect`: optional redirect behavior. Omit it to use `true`.
- `body`: optional text request body.

`body` is sent as text bytes. The method is never inferred from `body`; a
request with `body` and no `method` still uses `GET`.

The response streams the upstream response status, headers, and body. Upstream
4xx and 5xx statuses are not converted to JSON API errors. Hop-by-hop headers
such as `Connection`, `Keep-Alive`, `Transfer-Encoding`, `Upgrade`, and headers
named by `Connection` tokens are not copied.

## Tests

```http
POST /proxies/delay
POST /proxies/download
POST /proxies/{digest}/delay
POST /proxies/{digest}/download
```

Collection tests always test every proxy in the current snapshot. Empty
collection tests return the snapshot version and an empty `results` array.
Collection requests accept optional `concurrency`. Positive values are used as
the node concurrency limit for that request. Omitted `concurrency` falls back to
runtime config: `delay.concurrency` defaults to `100`, and
`download.concurrency` defaults to `1`. Explicit `0` or negative `concurrency`
returns `400`.
Single-proxy test requests do not accept `concurrency`; strict JSON decoding
rejects it as an unknown field.

Collection delay request:

```json
{
  "concurrency": 4,
  "headers": {
    "Cache-Control": "no-cache"
  },
  "timeout": 3000,
  "follow-redirect": true,
  "expected": "*",
  "rounds": 2,
  "unified": true,
  "urls": [
    "https://www.google.com/generate_204",
    {
      "url": "https://cp.cloudflare.com/generate_204",
      "headers": {
        "X-Test": "one"
      },
      "timeout": 1000,
      "follow-redirect": false
    }
  ]
}
```

Collection download request:

```json
{
  "concurrency": 2,
  "headers": {
    "Range": "bytes=0-1048575"
  },
  "timeout": 10000,
  "follow-redirect": true,
  "rounds": 1,
  "max-bytes": 104857600,
  "urls": [
    "https://cachefly.cachefly.net/50mb.test",
    {
      "url": "https://example.com/file.bin",
      "headers": {
        "Authorization": "Bearer token"
      },
      "max-bytes": 1048576
    }
  ]
}
```

For request-provided URLs, defaults are applied in this order:

```text
URL item field > request root field > runtime config field > internal default
```

Delay request `timeout` and `rounds` fields, at the request root or inside
`urls`, are optional positive numbers. Download request `timeout`, `rounds`, and
`max-bytes` fields, at the request root or inside `urls`, are optional positive
numbers. Omit these fields to use the fallback order above; explicit `0` or
negative values return `400`.

Headers are deep-merged case-insensitively across those layers. Higher
precedence values replace lower precedence values with the same header name.
Delay uses `HEAD`; download uses `GET`.

Delay tests run multiple URLs concurrently. Download tests run multiple URLs
serially. Delay and download both run rounds serially within each URL. Delay
`unified` second requests remain serial inside the same round.

Download HTTP statuses below `200` or greater than or equal to `400` are failed
download rounds. They are reported as test data, not JSON API errors.
For successful HTTP statuses, a download round measures bytes received divided
by elapsed seconds. If the body read stops because the request timeout expires
after receiving bytes, those bytes still produce a successful speed sample.
A round fails when the request fails before a successful response, the HTTP
status is outside the successful range, or no bytes are received.

Single delay response:

```json
{
  "version": 3,
  "digest": "canonical_endpoint",
  "delay-min": 120,
  "delay-max": 180,
  "delay-avg": 130,
  "delay-cost": 145,
  "success": 3,
  "failed": 1,
  "total": 4
}
```

Collection delay response:

```json
{
  "version": 3,
  "results": [
    {
      "digest": "canonical_endpoint",
      "delay-min": 120,
      "delay-max": 180,
      "delay-avg": 130,
      "delay-cost": 145,
      "success": 3,
      "failed": 1,
      "total": 4
    }
  ]
}
```

Single download response:

```json
{
  "version": 3,
  "digest": "canonical_endpoint",
  "speed-min": 700000,
  "speed-max": 1048576,
  "speed-avg": 900000,
  "speed-score": 675000,
  "success": 3,
  "failed": 1,
  "total": 4
}
```

All-round failure is still HTTP `200`; failed metrics use `-1` and include
`error`.

Proxy request errors before an upstream response is available use the JSON error
shape. Missing proxy digest returns `404`, invalid request fields return `400`,
and proxy request timeout returns `504`.
