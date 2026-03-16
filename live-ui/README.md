# Koala Live UI

Koala Live is the consumer-facing frontend for the Koala home security system.

This app is intentionally distinct from BearClaw Web:

- BearClaw Web: administrative control plane
- Koala Live: consumer-friendly home monitoring surface

Current scope:

- home status
- package checks
- camera roster and health
- recent activity timeline
- saved moments stored locally until consumer recording APIs exist
- profile/preferences stored locally until consumer account APIs exist

## Stack

- React
- TypeScript
- Vite

## Run

```bash
npm install
npm run dev
```

## Build

```bash
npm run lint
npm run build
```

## Environment

```bash
VITE_KOALA_API_BASE_URL=
VITE_KOALA_TOKEN=
```

If these are unset, the app runs in preview mode.
