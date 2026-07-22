# Cocoa Mobile Backend (Go)

Farmer-facing backend API for the **Cocoa Supply Chain Databank** (Is Thai Cacao). Serves the Flutter mobile app: authentication, farm/plot/hub/station registration, harvest & batch records, and dynamic form submission.

> **Heads-up for anyone reading the old docs:** this service is **Gin + GORM**, not the "Clean Architecture / service-layer" design the previous README described. Business logic lives inside the handlers; there is no `services/` layer. The structure below reflects the actual code.

---

## Role in the 2026–2027 plan

This repo is one component of a larger system being modernized over a 10-month thesis (Jul 2026 – Apr 2027), in two phases:

- **Phase I (mandatory, by Dec 2026)** — Add a **LINE OA AI chatbot** as a new conversational data-entry channel for farmers, modernize the farmer app, add SSO (LINE ↔ web), reminders, and web submission history. The existing form-filling system **stays in place unchanged** — the chatbot is additive, so there is **no data migration**. Technical-debt refactoring runs alongside every sprint (see the [fix dependency map](https://github.com/Cocoa-CUSAR-dev) / docs site).
- **Phase II (gated, Dec 2026 – Apr 2027)** — Knowledge Base + Computer Vision cocoa-disease detection.

**Where this service fits:** it is the **farmer-side backend**. In Phase I it is a refactor target (see the `GO-*` items in the weak-point register), and the new LINE OA chatbot / NLU work connects through this farmer-side backend and the shared database. Keep changes here compatible with the existing mobile app until the app refactor lands.

---

## Tech stack

- **Language:** Go
- **HTTP:** Gin (`github.com/gin-gonic/gin`)
- **ORM / DB:** GORM (`gorm.io/driver/postgres`) against PostgreSQL (NeonDB)
- **Auth:** JWT (`github.com/golang-jwt/jwt/v5`) delivered as a cookie
- **Containerization:** Docker + Docker Compose
- **Port:** `8080` (hardcoded in `cmd/main.go`)

## Project structure

```
cmd/
└── main.go                    # entry point + all route definitions; r.Run(":8080")
internal/
├── database/
│   └── postgres.go            # GORM connection + pool, configured from .env
├── handlers/                  # API entry points AND business logic (no separate service layer)
│   ├── agriculture_handler.go # farmer, farm, plot
│   ├── auth_handler.go        # register, login, GetMe, token generation
│   ├── collection_handler.go  # hub collector, hub, harvest
│   ├── form_handler.go        # tasks + dynamic form submission
│   ├── processing_handler.go  # processor, processing station, batch
│   └── ref_handler.go         # dropdown / reference / constants data
├── middleware/
│   └── auth_middleware.go     # validates the JWT cookie, puts user_id in context
└── models/                    # GORM structs (auth, farmer, farm, plot, batch, harvest, hub, ...)
.env                           # secrets — NOT committed (see .env.sample)
```

## First-time setup

1. Copy the env template and fill in real values:
   ```bash
   cp .env.sample .env
   ```
   Required keys are documented in [`.env.sample`](.env.sample): NeonDB connection (`DB_*`), `PORT`, `GIN_MODE`, and `JWT_*`.

2. Run with Docker:
   ```bash
   docker compose up -d --build
   ```

The API starts on `http://localhost:8080`.

Code quality: `go fmt ./...`, `go vet ./...`.

## Auth model

JWT signed with `JWT_KEY`, delivered as a **cookie** named `JWT_NAME`. Public routes: `/public/login`, `/public/register`, `/public/test`. Everything else is protected — the middleware validates the cookie and extracts `user_id` into the Gin context.

> **Known gap (Phase I refactor):** the JWT currently carries only `user_id`, not roles, so protected endpoints don't do role-based checks. See `GO-2` in the weak-point register.

## Known issues tracked for Phase I

- `GO-1` — GORM here vs. jOOQ in the Kotlin backend against the *same* database: two independent model definitions with no shared contract. After any schema change, update the models in `internal/models/` by hand and confirm they still match.
- `GO-2` — roles not in the JWT (above).
- `GO-5` — port `8080` hardcoded; no health-check endpoint beyond `/public/test`.

Full list, severities, and fix order: the project docs site.

---

**Security note:** never commit `.env`. The real NeonDB password and JWT signing key belong only in your local `.env`, which is gitignored.
