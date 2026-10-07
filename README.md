# Smart Invest Solutions — Backend API

A modern Go backend API for Smart Invest Solutions, built with clean architecture principles.

## 🛠️ Tech Stack

- **Language**: Go 1.22+
- **HTTP Framework**: [Gin](https://github.com/gin-gonic/gin)
- **Database**: [MongoDB Atlas](https://www.mongodb.com/atlas) via [Official MongoDB Go Driver v2](https://go.mongodb.org/)
- **Logging**: [Zerolog](https://github.com/rs/zerolog)
- **Password Hashing**: bcrypt via `golang.org/x/crypto`

## 📁 Project Structure

```
smart_invest_solutions_app_backend/
├── cmd/
│   └── server/
│       └── main.go              # Application entry point
├── internal/
│   ├── config/
│   │   └── config.go            # Environment configuration
│   ├── database/
│   │   └── mongodb.go           # MongoDB connection & client
│   ├── domain/
│   │   └── user.go              # Domain entities & interfaces
│   ├── repository/
│   │   └── user_repository.go   # MongoDB data access layer
│   ├── service/
│   │   └── user_service.go      # Business logic layer
│   ├── handler/
│   │   └── user_handler.go      # HTTP request handlers
│   ├── middleware/
│   │   └── cors.go              # CORS, logging & recovery middleware
│   └── router/
│       └── router.go            # Route definitions
├── pkg/
│   └── response/
│       └── response.go          # Standardized API responses
├── migrations/
│   └── migrations.go            # Database migration runner
├── .env.example                 # Environment variable template
├── .gitignore                   # Git ignore rules
├── Makefile                     # Build & dev commands
├── go.mod                       # Go module definition
└── go.sum                       # Go dependency checksums
```

## 🚀 Getting Started

### Prerequisites

- Go 1.22 or later
- MongoDB Atlas account (or local MongoDB instance)

### Setup

1. **Clone and navigate to the project:**
   ```bash
   cd smart_invest_solutions_app_backend
   ```

2. **Copy the environment file and configure:**
   ```bash
   cp .env.example .env
   ```
   Edit `.env` and set your MongoDB Atlas connection string.

3. **Install dependencies:**
   ```bash
   go mod tidy
   ```

4. **Run the server:**
   ```bash
   make run
   # or
   go run ./cmd/server/
   ```

5. **Development mode (with hot reload):**
   ```bash
   # Install air first: go install github.com/air-verse/air@latest
   make dev
   ```

### Verify

```bash
curl http://localhost:8080/health
```

## 📡 API Endpoints

### Health Check
| Method | Endpoint   | Description          |
|--------|-----------|----------------------|
| GET    | `/health` | Server health status |

### Users (v1)
| Method | Endpoint                    | Description            |
|--------|----------------------------|------------------------|
| POST   | `/api/v1/users/register`   | Register a new user    |
| GET    | `/api/v1/users`            | List all users (paginated) |
| GET    | `/api/v1/users/:id`        | Get user by ID         |
| PUT    | `/api/v1/users/:id`        | Update user            |
| DELETE | `/api/v1/users/:id`        | Delete user            |

### A client is told when their advisor's access lapses

`GET /users/me/advisor` carries `access_expired` and `access_expires_at` alongside the contact details.

It reports the **advisor's** standing, never the client's. An expired admin is refused at login *and*
on every request after it (`middleware.AccountGuard`), so they genuinely cannot sync a due list, link
an imported row or edit a record until a super admin renews them — which means the updates that used
to arrive on the client's portfolio have stopped. The client's own account is untouched: their data,
their documents and everything they maintain themselves keep working exactly as before, and the guard
never looks at the caller's agency. So the app says "agency updates are paused", not "your account has
a problem".

The expiry flag reuses the same rule as login, so a client is never told their advisor is fine about
somebody the platform is already refusing. `nil` means no expiry rather than an expired one — admin
accounts created before validity existed carry no date, and reading that as expired would warn every
one of their clients for no reason. A super_admin agency never expires and carries no date at all.

### Document categories can be typed, not just picked

The vault's category list ends in **Others**, which is never stored as-is: picking it asks the client
what kind of document it is and stores *that*, so a file shows up as "Marriage Certificate" rather than
filed under a vague "Others".

That makes the category free text, so `normalizeDocumentCategory` is the one place that decides what it
may look like — whitespace collapsed, trimmed, defaulted to "General" when blank, and capped at 40
**runes** rather than bytes, so a Bengali or Hindi label isn't cut to a third of its length or sliced
mid-character. It runs on both the upload and the update path, since a file can be switched to a typed
category after the fact.

### Password reset tells you when the email isn't registered

`POST /users/forgot-password` answers **404** with "No account is registered with …" rather than the
usual generic "if an account exists, a code has been sent".

That is a deliberate trade-off, not an oversight. The generic answer hides which addresses are
registered, but it also leaves somebody who simply mistyped their email staring at a screen that
promised a code, waiting for an email that was never coming. The product choice here is to tell them.
What makes it safe enough:

- the route is **rate limited per IP** (`middleware.RateLimit`, 6 requests a minute), so the answer
  can't be used to test addresses in bulk — far more than a person resetting their own password needs,
  far less than enumeration needs to be worth doing;
- login itself still gives nothing away: a wrong password reads exactly like an unknown account there,
  so the one place an attacker would actually use a known address remains silent.

Two more refusals, both **400**, exist because sending a code would otherwise be a dead end: an
account retired by a family merge (it can never sign in again whatever its password is), and a client
signup whose email was never verified (login refuses it regardless, so finishing signup is the step
that actually unblocks them). Admin accounts are created without the verified flag and are
deliberately *not* caught by that rule — an admin locked out needs this flow to work.

The limiter keeps its counters in memory, which is the right scope for a single-process deployment;
behind several instances each would allow the quota separately, so a shared store would be the next
step if this ever runs replicated.

### Onboarding and the Agency ID

There is one code on this platform. An admin's **Admin ID** (e.g. `ADM-7F3K9Q`) is also their
**Agency ID**, and it is the only value onboarding accepts: a client who types it at signup (or on an
access request) is filed under that agency *and* credited to that admin in the referral report.

The separate advisor referral code has been retired — it was a second value doing the same job, which
meant two inputs on the signup form with no way for an applicant to know which one mattered. Migration
12 drops `users.referral_code` (and its unique index) and `access_requests.applied_referral_code`.
Referral records already filed are untouched: they point at the referrer's account rather than at a
code, so the super admin's report keeps its full history.

An Agency ID that names no admin is **refused at submission** rather than stored, so a typo can never
strand a signup in an agency nobody manages. One that names a deactivated or merged staff account
still resolves — that is a management problem for the super admin — but earns that account no referral
credit.

**A Super Admin chooses the Admin ID, or has one generated.** `POST /admins` takes an optional
`admin_id`: supply one to pick a memorable ID a client can actually remember typing (`ADM-ASHA01`), or
leave it out and a random `ADM-XXXXXX` is generated. `GET /admins/next-id` returns a free one for the
form's "generate" button — it reserves nothing, since uniqueness is settled at creation.

What is accepted is deliberately forgiving about *how* it is typed and strict about what gets stored:
case is ignored, spaces and inner dashes are dropped, the `ADM-` prefix may be omitted or doubled, and
the body must be 3–12 letters or digits. The canonical `ADM-` form is what reaches the database,
because the client app validates the Agency ID box against that same shape — an ID that slipped
through here would be one no client could ever type.

Uniqueness is checked before insert *and* enforced by the unique sparse index on `users.admin_id`,
which is the only thing that settles two Super Admins creating accounts in the same moment. A
duplicate-key error now names the field that actually collided rather than always blaming the email.

The ID is fixed once created. Every client of that agency stores it as their own `agency_id`, so
changing it later would silently orphan them — reassigning a client's agency is done per client from
their own page instead.

### Per-client catalog visibility (v1)
| Method | Endpoint                               | Description                               |
|--------|---------------------------------------|-------------------------------------------|
| GET    | `/api/v1/users/:id/product-access`    | Which catalog products this client sees   |
| PUT    | `/api/v1/users/:id/product-access`    | Set which products this client sees       |

The product catalog is platform-wide — a super admin curates it, and every published product is
offered to every client by default. This is the per-client override an admin applies on top:
`mode: "all"` is the whole published catalog, `mode: "selected"` limits the client to `product_ids`.
An empty `selected` list is legitimate and means that client sees no products; it is not the same as
never having been configured, which the response reports as `configured: false`.

A client with no record sees everything, so enabling this never empties anyone's Products tab. A
plain admin may only set it for a client of their own agency; a client outside it reads as not found
rather than forbidden, so the endpoint can't be used to enumerate other admins' clients. Every
submitted product ID is verified to exist and the whole save is rejected if one doesn't — a silently
dropped ID would leave the admin believing they had granted a product the client never receives.

The restriction is enforced in **both** catalog doors: `GET /products` filters inside the database
query (so the page size and the reported total both count only what the caller may see), and
`GET /products/:id` answers "product not found" for a hidden one, so a link or a cached list can't
walk past the list filter. It only ever narrows — a granted but unpublished product stays hidden —
and agency staff are never restricted. Deleting a product drops it from every client's list.

### Agency scoping on the admin listings

The four master lists (`/life-insurances`, `/health-insurances`, `/general-insurances/all`,
`/fixed-deposits`) all take an `agency_id` query parameter, honoured **only for a super_admin**: one
Agency ID, `unassigned` for records whose owner belongs to no agency, or omitted for the whole
platform. A plain admin is always pinned to their own agency whatever they send, and an admin whose
agency can't be resolved sees nothing rather than everything. The filter is applied inside the
aggregation that also produces the count, so the page and the total always describe the same set.

### Agency Sync acts on one agency's book

Agency Sync used to refuse a super_admin outright. It now takes the agency being acted for, passed as
`agency_id` (a query parameter on the reads; also accepted as a form field on the two multipart
uploads):

- A **plain admin** has exactly one book and the server uses it whatever they send.
- A **super_admin** has none of their own, so they name the agency. An omitted one is refused with
  "choose which agency this applies to" rather than guessed at — an upload with no agency would write
  inbox rows nobody could ever claim. An Agency ID that names no staff account is refused too.

Linking is checked against the **agency being acted for**, not against the caller's role: the inbox row
and the target client must belong to the same book. So a super_admin importing for one agency cannot
attach that row to another agency's client, which is the same rule an admin follows.

### Screen banners (v1)
| Method | Endpoint                     | Description                                  |
|--------|-----------------------------|----------------------------------------------|
| GET    | `/api/v1/announcements`     | The banners on *this* caller's screen        |
| GET    | `/api/v1/announcements/all` | Every banner, with status (super admin)      |
| POST   | `/api/v1/announcements`     | Create — multipart, optional image or video  |
| PUT    | `/api/v1/announcements/:id` | Patch                                        |
| DELETE | `/api/v1/announcements/:id` | Remove, purging the media                    |

A banner carries a title, an optional line of description, an optional image **or video**, an optional
http(s) tap-through, an audience, a schedule and a priority. Curating them is **super_admin only**: a
banner reaches every device on the platform, so one agency's admin does not get to broadcast.

**The audience comes from the caller's role, never from the request.** A client asking for the staff
notices gets the clients' ones. `all` always applies; `clients` and `admins` exist so an offer written
for clients doesn't clutter an admin dashboard they can't act on, and an internal notice isn't leaked
to clients. An unrecognised audience is **refused rather than defaulted to `all`** — quietly widening
a typo would put a staff notice on every client's screen.

**The schedule is filtered in the query**, so a draft or an expired banner never reaches a device at
all. Both bounds are optional: no start means "from now", no end means "until switched off" — which
relies on `{field: nil}` matching documents where the field is absent, verified against the live
server. The management list reports a **derived** status (live / scheduled / expired / paused) so it
can't disagree with the dates printed beside it.

**Only http(s) links are accepted.** Any other scheme is one the app would hand to the operating
system, which is how a banner becomes a way to open an arbitrary deep link on somebody's phone.

#### Video needed real storage work

`UploadDocumentWithCompression` applies an **image** transformation to anything non-PDF, and
`DeleteImage` calls Cloudinary's `Destroy` with no `resource_type` — which defaults to `image`, so
**deleting a video was accepted and silently did nothing**, leaving the file in storage forever. So
`UploadMedia` / `DeleteMedia` were added:

- the resource type is detected from the file's own **bytes**, not its filename, and **persisted** on
  the record — because a delete has to name the type, and re-guessing it later is how assets leak;
- the image transformation is skipped for video (transcoding it is slow and lossy for no gain) and
  video gets a longer timeout, since even a short clip takes longer to transfer than a photo;
- a video's **poster frame** is derived by asking the provider for the same asset as a JPEG — no extra
  storage, and the banner has something to show before anyone presses play;
- replacing media purges the old asset **with its own stored type**, and only after the record points
  at the new one. A failed upload leaves the existing banner untouched; a failed insert cleans up the
  orphaned file it just uploaded.

### Renewal book (v1)
| Method | Endpoint            | Description                                      |
|--------|--------------------|--------------------------------------------------|
| GET    | `/api/v1/renewals` | What is due and when, across every instrument    |

One merged, urgency-ordered list of every premium due, policy expiring and deposit maturing, from 90
days overdue through the next 90 days. Each row carries `days_left` (negative when overdue), the
client who holds the record, and the **admin whose agency they belong to** — so a super admin can see
whose renewals are slipping. Query parameters: `q`, `agency_id` (an Agency ID or `unassigned`, super
admin only), `kind` (`life`/`health`/`motor`/`deposit`), `window`
(`overdue`/`7`/`30`/`90`/`all`), `page`, `limit`.

The four instruments store their dates differently — life and health on
`premium_details.next_due_date`, health also on `policy_details.expiry_date`, deposits on
`maturity_date`, and motor on `date_of_expiry` as a `YYYY-MM-DD` **string** (compared as a string
range, which is correct in that format). Each is filtered by date **in the database** before the owner
is joined, so the horizon bounds the work however large the book gets.

Three rules the service enforces, and why:

- **A premium past the end of cover is not a demand.** A due date beyond the premium paying term or
  maturity is a stale schedule, so it is dropped — the same rule the client's own dashboard applies,
  so an admin is never chasing money the client doesn't owe.
- **A health policy shows whichever comes first**, its premium or its expiry. Chasing a premium due
  next month is pointless if the cover lapses next week.
- **A day window includes everything overdue.** "The next 7 days" that hid last week's misses would
  hide the most urgent work on the screen.

`summary` counts the whole scope rather than the page — including `unassigned`, the rows whose client
belongs to no agency and so has nobody chasing them — so the counters hold still as the reader
switches `window` and `kind`.

### Client map (v1)
| Method | Endpoint             | Description                                        |
|--------|---------------------|----------------------------------------------------|
| GET    | `/api/v1/client-map` | Clients with their holdings, agency and app presence |

One row per client: how many life / health / motor policies and deposits they hold, which admin's
agency they belong to, and whether they have ever signed in to the app. Query parameters: `q`,
`agency_id` (an Agency ID, or `unassigned`), `app` (`on_app` / `not_on_app` / `no_access`),
`holdings` (`with` / `without`), `page`, `limit`.

A super admin sees every client and may aim the view at one agency; a plain admin always gets their
own agency, whatever the request asks for. `summary` totals the whole filtered scope rather than the
page, so the counters hold still while the reader switches `app` and `holdings`.

**App presence is measured, not guessed.** Every successful sign-in stamps `last_login_at` and
increments `login_count` on the account (`UserRepository.RecordLogin`), and presence is derived from
that: an account that cannot sign in is `no_access`, one with a sign-in on record is `on_app`, and
everything else is `not_on_app`. A super admin impersonating a client deliberately does **not** stamp
it — otherwise support activity would read as client activity. Sign-ins are only recorded from the
day this shipped, so a client who last used the app before then reads as `not_on_app` until their
next sign-in.

## 🏗️ Architecture

This project follows **Clean Architecture** principles:

- **Domain Layer** (`internal/domain/`): Core entities, DTOs, and interface definitions
- **Repository Layer** (`internal/repository/`): MongoDB data access implementations
- **Service Layer** (`internal/service/`): Business logic orchestration
- **Handler Layer** (`internal/handler/`): HTTP request handling
- **Router Layer** (`internal/router/`): Route setup and dependency wiring

## 📋 Available Make Commands

```bash
make build    # Build the application binary
make run      # Build and run the application
make dev      # Run with hot reload (requires air)
make test     # Run all tests
make vet      # Run go vet
make lint     # Run golangci-lint
make clean    # Remove build artifacts
make deps     # Download dependencies
make check    # Run vet + lint + tests
```
