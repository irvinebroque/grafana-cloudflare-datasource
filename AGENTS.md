# Development

Use the Cloudflare CLI (`cf`) for Cloudflare development and resource management where supported. Discover commands with `cf cli search` and consult https://developers.cloudflare.com/cf/get-started/.

Use Vite Plus (`vp`) for TypeScript builds, tests, formatting, linting, and type checking. Keep TypeScript strict. All generic anti-slop rules in `vite.config.ts` must remain enabled.

Run `vp check`, `vp test run`, `go test -race ./...`, and `vp run build` before delivery. Live validation must distinguish fixtures from real Cloudflare results. Never commit credentials or account-specific captured data.
