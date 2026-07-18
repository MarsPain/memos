# Build The Hardened Provider Transport

Parent spec: [AI Foundation](../../../docs/product-specs/ai-foundation.md)
Type: task
Status: ready-for-agent
Blocked by: 01

## Outcome

All AI provider calls and connectivity tests use one cancellable, bounded, SSRF-aware HTTP transport with normalized safe failures.

## Scope

- Validate HTTP/HTTPS endpoint syntax and reject userinfo, fragments, malformed hosts, and secret-bearing parameters.
- Resolve and validate destinations before dialing; revalidate redirects and their resolved destinations.
- Deny loopback, link-local, and private-network destinations unless the persisted administrator opt-in allows them.
- Bound DNS/dial, connection, redirect count, request size, response size, and total request time.
- Propagate caller cancellation and bound retries.
- Normalize configuration, authentication, rate-limit, timeout, unavailable, malformed-response, and internal errors.
- Ensure logs and returned errors redact credentials, private request content, and provider payloads.

## Acceptance

- Runtime calls and connectivity tests share the same enforcement path.
- Redirects and DNS destination changes cannot bypass network policy.
- Explicit private-network authorization enables local models without widening the default boundary.
- Failure categories are stable and safe for API and UI consumption.

## Verification

Use deterministic HTTP fixtures for allowed and blocked endpoints, redirects, DNS changes, cancellation, timeout, retries, malformed responses,
oversized bodies, and explicit private access. Run `go test -v -race ./internal/...`.

## Comments

No comments yet.
