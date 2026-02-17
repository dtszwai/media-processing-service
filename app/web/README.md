# Media Processing Service - Web UI

Web interface for the Media Processing Service built with Svelte 5, TypeScript, and Tailwind CSS.

## Features

- Tenant registration/login with token refresh
- API key management UI
- Image and PDF upload via drag and drop
- Direct upload (<= 50MB) and presigned upload (> 5MB, up to 1GB)
- Image derivative generation (`jpeg/png/webp`) with width controls
- PDF derivative generation (`document.preview`, `document.text`)
- Asset-centric result viewer (download, retry failed jobs, status polling)
- Per-asset short URL creation/revocation
- Media library with lazy-loaded thumbnails
- Analytics dashboard and DLQ admin tooling

## Routes

| Path | Page | Description |
| --- | --- | --- |
| `/login` | `LoginPage` | Tenant login |
| `/register` | `RegisterPage` | Tenant + admin registration |
| `/media/images` | `MediaPage` | Image upload and media library |
| `/media/documents` | `MediaPage` | PDF upload and document processing |
| `/analytics` | `AnalyticsPage` | Usage metrics and leaderboards |
| `/settings/api-keys` | `ApiKeysPage` | API key management |
| `/admin/dlq` | `DlqPage` | Dead letter queue operations |

## Development

```bash
# Install dependencies
pnpm install

# Start dev server (port 3001)
pnpm dev

# Type check
pnpm check

# Build for production
pnpm build
```

### Environment Variables

Configure the API backend URL and document limits:

```bash
# .env.local (optional, defaults to localhost:9000)
VITE_API_URL=http://localhost:9000
VITE_DOCUMENT_MAX_PAGES=200
```

## Testing

```bash
# Unit tests (Vitest)
pnpm test              # Run once
pnpm test:watch        # Watch mode

# E2E tests (Playwright)
pnpm test:e2e          # Run headless
pnpm test:e2e:ui       # Interactive UI mode
```

**Test structure:**

- `src/**/*.test.ts` - Unit tests for services, utils, and stores
- `e2e/*.spec.ts` - E2E tests for user flows

## Project Structure

The codebase follows a **Domain-Driven Design (DDD)** feature-based architecture:

```
src/
├── features/                 # Feature modules (bounded contexts)
│   ├── media/                # Media upload & management feature
│   │   ├── components/       # UploadZone, MediaList, ResultSection
│   │   ├── pages/            # MediaPage
│   │   ├── services/         # media.service.ts (API calls)
│   │   ├── queries/          # media.queries.ts (TanStack Query)
│   │   ├── stores/           # currentMediaId, isProcessing
│   │   └── index.ts          # Barrel export
│   ├── analytics/            # Analytics dashboard feature
│   │   ├── components/       # Dashboard, Charts, Tables, Modal
│   │   ├── pages/            # AnalyticsPage
│   │   ├── services/         # analytics.service.ts
│   │   ├── queries/          # analytics.queries.ts
│   │   └── index.ts          # Barrel export
│   ├── auth/                 # Login/register/user/api-key flows
│   ├── shorturl/             # Short URL service integration
│   └── admin/                # DLQ admin page + queries/services
│
├── shared/                   # Cross-cutting concerns
│   ├── components/           # Header (shared UI)
│   ├── config/               # Environment variables (env.ts)
│   ├── http/                 # HTTP client utilities
│   ├── queries/              # Query client, keys, health queries
│   ├── types/                # TypeScript types, Zod schemas, errors
│   ├── utils/                # Formatting, image utilities
│   └── index.ts              # Barrel export
│
├── App.svelte                # Root component with routing
├── app.css                   # Global styles
└── main.ts                   # Entry point
```

### Feature Module Structure

Each feature module is self-contained with its own:

| Directory     | Purpose                                |
| ------------- | -------------------------------------- |
| `components/` | UI components specific to the feature  |
| `pages/`      | Page-level components (route targets)  |
| `services/`   | API calls (fetch logic)                |
| `queries/`    | TanStack Query hooks for data fetching |
| `stores/`     | Svelte stores for feature state        |
| `index.ts`    | Barrel export for clean imports        |

## Code Style & Patterns

### Architecture

- **DDD feature modules** - Each feature (media, analytics, auth, admin, shorturl) is isolated
- **Layered architecture** - Components → Queries → Services → API
- **Shared infrastructure** - Cross-cutting concerns in `shared/` (types, utils, http, queries)
- **Custom routing** - Simple path-based routing in App.svelte (no external router)
- **Auth-aware routing** - Public routes (`/login`, `/register`) and guarded app routes
- **Centralized state** - Svelte stores for auth and media workflow state
- **Separated API layer** - All fetch calls in services, components use queries
- **Zod validation** - API responses validated with Zod schemas
- **Barrel exports** - Clean imports via index.ts files

### Svelte 5

- Use `$state()` for local reactive state
- Use `$derived()` for computed values
- Use `$effect()` for side effects
- Use `$props()` for component props with TypeScript interfaces

### TypeScript

- Define types in `shared/types/` directory
- Use Zod schemas for API response types (type-safe validation)
- Use explicit return types on exported functions
- Prefer `interface` over `type` for object shapes
- Import from barrel exports: `import { type Media } from "../shared"`

### CSS

- Tailwind for utility classes in components
- Custom CSS in `app.css` for reusable patterns (`.btn-primary`, `.status-badge`, etc.)
- CSS variables in `:root` for theme colors

### Code Guidelines

- Keep functions small and focused
- Extract shared logic into helper functions
- Handle errors at the call site with try/catch
- Use early returns to reduce nesting

## Tech Stack

- [Svelte 5](https://svelte.dev/) - UI framework with runes
- [TanStack Query](https://tanstack.com/query) - Data fetching and caching
- [TypeScript](https://www.typescriptlang.org/) - Type safety
- [Zod](https://zod.dev/) - Runtime validation
- [Tailwind CSS v4](https://tailwindcss.com/) - Styling
- [Vite](https://vite.dev/) - Build tool
- [Vitest](https://vitest.dev/) - Unit testing
- [Playwright](https://playwright.dev/) - E2E testing
