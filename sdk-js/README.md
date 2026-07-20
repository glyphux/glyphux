# @glyphux/sdk

A typed JS/TS client for Glyphux's `/api/v0` HTTP API. Wraps content
CRUD/publish/versioning, media upload/serve, auth, and user management —
the same surface any other HTTP client of `glyphuxd` sees, just typed.

Requires Node 24+ (uses the built-in `fetch`, `FormData`, and `Blob`
globals — no `axios`/`node-fetch` dependency).

## Install

This package is not yet published; consume it from a workspace/local path,
e.g. `npm install ../sdk-js` or a `file:` dependency, and build it first:

```sh
cd sdk-js
npm install
npm run build
```

## Quick start

```ts
import { GlyphuxClient } from "@glyphux/sdk";

const client = new GlyphuxClient({ baseUrl: "http://localhost:8080" });

// Authenticate. On success the client's bearer token is set automatically,
// so every call made afterwards on this client instance is authenticated.
const { token } = await client.auth.login("admin@example.com", "hunter2!!");

// Or skip login and hand an existing token straight to the constructor —
// handy for server-to-server / script use once you already have one:
// const client = new GlyphuxClient({ baseUrl: "...", token });
```

## Content

```ts
const post = await client.content.create("post", { title: "Hello world" });
await client.content.update("post", post.id, { title: "Hello, world!" });
await client.content.publish("post", post.id);

const posts = await client.content.list("post"); // ?locale= optional 2nd arg
const one = await client.content.get("post", post.id);

const versions = await client.content.listVersions("post", post.id);
await client.content.rollback("post", post.id, versions[0].version);

await client.content.unpublish("post", post.id);
await client.content.delete("post", post.id);
```

## Media

```ts
const bytes = await fs.promises.readFile("photo.jpg");
const asset = await client.media.upload(bytes, "photo.jpg");

const meta = await client.media.get(asset.id);
const all = await client.media.list();

// Binary response — returned as a raw Response so the caller decides how
// to consume it (.blob(), .arrayBuffer(), streaming, Content-Type, etc.).
const original = await client.media.file(asset.id);
const thumb = await client.media.file(asset.id, { width: 200, height: 200 });

await client.media.delete(asset.id);
```

## Auth

```ts
const me = await client.auth.me(); // throws GlyphuxApiError(401) if unauthenticated
await client.auth.logout();
```

## Users (admin-only)

```ts
const editor = await client.users.create("editor@example.com", "hunter2!!", "editor");
const everyone = await client.users.list();
```

## Errors

Every non-2xx response throws a `GlyphuxApiError`:

```ts
import { GlyphuxApiError } from "@glyphux/sdk";

try {
  await client.content.create("post", {}); // missing required "title"
} catch (err) {
  if (err instanceof GlyphuxApiError) {
    console.log(err.status); // 422
    console.log(err.issues); // ["field \"title\" is required"]
  }
}
```

## Testing

`npm test` runs the suite against a **real** `glyphuxd` binary: it builds
the daemon, boots it against a scratch data directory, completes first-run
setup over real HTTP, and runs every test as a real network call against
that live process — no mocked `fetch`.

One current caveat baked into the test harness (`test/global-setup.ts`,
`testdata/seedcomposition/`): the daemon has no HTTP-level way to declare a
content type after first-run setup completes today (the wizard's own form
only collects site name, admin credentials, and the database choice). The
test harness works around this the same way the Go test suite does —
seeding a `post` content type directly via `internal/composition.Store.Save`
— using a small helper Go program rather than touching the SQLite file
directly.
