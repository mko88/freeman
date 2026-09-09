# Freeman

An API client. Build requests, send them, read the responses — and keep
everything as plain JSON files in a folder you choose, so your API
collection lives in git next to the code it tests.

Runs as a desktop app on Windows and Linux, or as a small server you
point a browser at.

---

## Install

Download the zip for your platform from
[Releases](https://github.com/mko88/freeman/releases), unzip it, and run
it. There is no installer.

| File | What it is |
|---|---|
| `freeman-*-windows-amd64.zip` | Desktop app for Windows |
| `freeman-*-linux-amd64.zip` | Desktop app for Linux |
| `freeman-server-*-linux-amd64.zip` | Headless server, for a container or a browser |

The desktop builds are not code-signed, so Windows SmartScreen will warn
on first launch — choose *More info* → *Run anyway*.

## First run

Freeman asks for a workspace folder. Pick an empty one, or an existing
workspace to reopen it. A new workspace starts with one collection
(*My Requests*) and one environment (*Development*).

It reopens the last workspace you had open. To switch, use
**Settings → Workspace → Change…**

## The workspace

Everything lives in the folder you picked:

```
your-workspace/
  collections/
    my-requests/
      collection.json      one file per collection: its requests, folders, bodies
  environments/
    development.json       variables
    development.local.json secret values — gitignore this
  theme.yaml               optional: colours
  headers.yaml             optional: header autocomplete
  .cache/responses/        the last response for each request
```

Readable, diffable, and safe to commit — except `*.local.json`, which is
where values marked **secret** are written. Freeman writes a `.gitignore`
into a new workspace that already excludes those and the response cache.

---

## Sending a request

Pick a method, type a URL, press **Enter** (or click **Send**).

The tabs under the URL bar each show what they hold, so you can see
what's set without opening them.

**Params** — query-string rows appended to the URL. Untick one to leave
it out without deleting it.

**Headers** — header rows, with autocomplete on both names and common
values.

**Auth** — one of:

- *Bearer* — a token
- *Basic* — username and password
- *API key* — a header name and value of your choosing
- *OAuth2* — the client-credentials grant. Freeman fetches a token when
  you send and reuses it until it expires

**Body** — one of:

- *none*
- *raw* — free text, with JSON/XML highlighting. Tab indents; press Esc
  then Tab to leave the editor
- *form-data* — text fields and file fields
- *x-www-form-urlencoded* — key/value pairs
- *binary* — a file sent as the whole body

**Options** — how the request is sent, as opposed to what is sent. Each
row has an ⓘ that explains it.

| Option | Default |
|---|---|
| Follow redirects | on — turn it off to get the 3xx itself |
| Maximum redirects | 10 |
| Send and store cookies | on — one shared jar, so signing in on one request authenticates the next |
| Timeout | 30 s |
| Skip TLS certificate check | off — accepts any certificate at all |
| Verify with a custom CA | off — with a PEM file, verifies against that *instead of* the machine's trust store |
| Client certificate | a PEM pair, for mutual TLS |

A certificate that won't verify has two answers, and they are not the
same. **Skip TLS certificate check** stops checking who the server is.
**Verify with a custom CA** keeps checking, against a file you name —
your internal root, or the intermediate a server forgets to send (every
certificate in the file is trusted as an anchor). Switched on it replaces
the machine's trust store for that request, so a public host that doesn't
chain to your file is refused while it's on. Prefer it to skipping.

Worth knowing if Freeman accepts something another client rejects: on
Windows it verifies through the OS, which can fetch a missing
intermediate the way Chrome and curl do. Node — so Postman, and probably
your CI — ships its own CA list and won't. If Postman says *unable to
verify the first certificate* and Freeman is happy, the server is sending
an incomplete chain, and Postman is the one telling you the truth.

**Code** — the request as a runnable script. See below.

## Environments

Variables written `{{name}}` are substituted into the URL, params,
headers, body, auth fields, and the client-certificate paths.

Switch the active environment with the picker in the top bar. Manage
them in **Settings → Environments**: expand a row to edit its variables.

New requests start from **Settings → Requests** — see below.

Tick **secret** on a variable and its value moves to
`<environment>.local.json`, so the environment itself stays safe to
commit.

## Reading the response

The response pane has its own **Body**, **Headers** and **Cookies**
tabs, with the status, time and size on the right. Hover any of them for
the exact value. **Cookies** lists what this response asked you to
store — name, value, domain, path, expiry and flags — read from its own
`Set-Cookie` headers, so it shows what the response did whether or not
the request was on the shared jar.

Three views, always available:

- **Pretty** — formatted and coloured; images render as images
- **Raw** — exactly what came back
- **Hex** — a hexdump with an ASCII gutter

gzip, deflate and brotli responses are decoded for you. A body too large
to show is written to a file instead, and the **⋯** menu will open it,
copy its path, or show it in your file manager.

Every response is cached under `.cache/responses`, so reopening a
request shows what it returned last time. Clear one from the **⋯** menu,
or all of them in **Settings → Workspace**.

## Generating code

The **Code** tab renders the current request as a script you can run
elsewhere: **bash** (curl), **powershell** (Invoke-RestMethod),
**python** (requests) or **javascript** (fetch). **Copy** puts it on the
clipboard.

The script sends what *Send* sends, including redirects, the redirect
cap, the timeout, and the TLS options. Two details worth knowing:

- Cookies are written in as a plain `Cookie` header — whichever ones the
  shared jar holds for that URL, when **Send and store cookies** is on.
  No file beside the script, no session variable, no import: paste it
  anywhere and it sends the session you're signed in with. The trade is
  that it's a snapshot, so regenerate after signing in again — and the
  cookie is readable in the script, worth knowing before pasting one
  into a ticket.
- A custom CA reaches bash (`--cacert`) and python (`verify=`). powershell
  and javascript have no per-request equivalent, so it's absent there
  rather than approximated.
- javascript also can't express the redirect cap or a client certificate;
  those are silently absent from that format only.

## Settings

Open with **⚙** in the top bar, or from **Edit …** at the top of either
picker.

- **Workspace** — which folder is open, clearing the response cache, and
  the two response-size limits
- **Collections** — create, rename and delete collections
- **Environments** — the same, plus each one's variables
- **Requests** — every setting on the Options tab, as a new request
  starts it. Only a starting point: a saved request carries its own
  answer, so changing one here doesn't reach back into requests you've
  already written. The timeout and the redirect cap are the exception —
  they're also the fallback for any request that never set one. The
  certificate paths take `{{variables}}`, which is usually the point of
  setting them here.
- **Cookies** — the shared jar every request with **Send and store
  cookies** on draws from: forget one, or empty it. It lasts until you
  clear it or close Freeman, so this is where you go to sign out of a
  session without restarting.
- **Appearance** — the two fonts and the interface scale, applied as you
  change them

### Fonts and scale

Two faces. The interface is proportional; the fixed-width one is kept
for the panes that hold a whole document — the raw request body, the
response body, and the generated code beside them — where indentation and
line-by-line reading are the point.

**Settings → Appearance** replaces either with a font installed on your
machine (leave a field empty for the bundled IBM Plex), and scales the
whole interface from 70% to 200%. **Ctrl +** and **Ctrl -** step the
scale from anywhere in the app, and **Ctrl 0** goes back to 100%. The scale moves spacing as well as
type, so it reads as one size rather than large text in boxes that stayed
put.

### Theming

Create `theme.yaml` in the workspace:

```yaml
active: midnight
themes:
  midnight:
    bg: "#0f1115"
    text: "#e6e6e6"
    accent: "#e8b339"
```

Any key you leave out keeps its value from the built-in dark palette.
The others are `bgPanel`, `bgElevated`, `bgHover`, `bgResponse`,
`border`, `borderSubtle`, `textMuted`, `success`, `warning`, `error`,
and the per-method colours `methodGet`, `methodPut`, `methodNeutral`.

### Header autocomplete

Create `headers.yaml` in the workspace to change what the header editor
suggests. `headers.example.yaml` in this repo is a starting point.

---

## Driving Freeman from a script

The desktop build serves a small HTTP API on `127.0.0.1:8090` that
drives the app itself — not just its data. A script can set a field,
click a tab, send a request, and read back exactly what's on screen.

```bash
curl -X POST http://127.0.0.1:8090/api/ui/action \
  -H 'Content-Type: application/json' \
  -d '{"action":"setRequestField","payload":{"field":"url","value":"https://example.com"}}'

curl http://127.0.0.1:8090/api/ui/state
```

- `GET /api/agent` returns Markdown documenting every action and route —
  point a script or an agent there first.
- The **?** button in the top bar shows the same catalogue in the app.
- The status bar at the bottom shows the address and a live log of the
  actions received.

Requests must send `Content-Type: application/json` with a body, and
anything a browser marks as cross-origin is refused, so a web page you
have open cannot drive the app.

Change the address with `FREEMAN_CONTROL_LISTEN`:

```
FREEMAN_CONTROL_LISTEN=127.0.0.1:9090 freeman
```

## Running it in a container

`freeman-server` is the same app without the desktop window, serving the
UI to a browser:

```
freeman-server -workspace ./workspace -listen 127.0.0.1:8080
```

`FREEMAN_WORKSPACE` and `FREEMAN_LISTEN` work as well. There is a
`docker-compose.yml` in this repo for running it that way.

It has no native file dialogs and no `ui:action` control API; everything
else is the same.

## Trying it without a real API

This repo ships a local test server that answers every kind of request
Freeman can send — redirects, cookies, delays, compression, all four
auth types, self-signed TLS and mutual TLS.

```
pwsh scripts/Start-TestServer.ps1     # or: py scripts/test_server.py
py scripts/seed_test_requests.py
pwsh scripts/Stop-TestServer.ps1
```

`seed_test_requests.py` fills a **Test Server** collection with 26 ready
-made requests — one per endpoint and per option worth trying — and an
environment pointing at it. It never overwrites a request you already
have.

---

## Building from source

Open the repo in its devcontainer (VS Code: *Reopen in Container*),
which has Go, Node and the Wails CLI.

```
bash build.sh                 # desktop app for Windows and Linux → bin/
bash build.sh --windows       # one platform only
bash build.sh --server        # the container server
bash build.sh --full          # add gofmt, go vet, go test and svelte-check
```

From a Windows host, `.\build-container.ps1` runs the same script inside
the container and takes the same flags (`-Windows`, `-Full`, …).

## Keyboard

| | |
|---|---|
| **Enter** in the URL bar | Send |
| **Tab** in the raw body editor | Indent |
| **Esc** then **Tab** | Leave the body editor |
| **Esc** | Close a menu or dialog |
