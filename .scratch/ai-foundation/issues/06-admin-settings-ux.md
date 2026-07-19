# Add AI Capability Settings UX

Parent spec: [AI Foundation](../../../docs/product-specs/ai-foundation.md)
Type: task
Status: resolved
Blocked by: 01, 02, 05

## Outcome

Administrators can configure and test generation and embedding assignments with clear capability state, disclosure, and safe endpoint controls.

## Scope

- Add Generation and Embedding sections to the existing AI settings page.
- Show provider/model dependencies, readiness, and actionable disabled states.
- Add the bounded connectivity-test action with sanitized results.
- Require acknowledgement that external embedding incrementally processes every eligible Memo chunk.
- Require explicit private-network authorization for new or edited private endpoints.
- Show all users whether external AI processing is enabled without exposing credentials.
- Preserve existing transcription settings and editor controls.

## Acceptance

- Invalid or incomplete assignments cannot be saved as ready capabilities.
- Disclosure precedes external embedding assignment.
- Private endpoint opt-in is explicit and visible.
- API keys remain write-only throughout load, edit, test, and save flows.
- Existing transcription UX remains unchanged except for additive capability context.

## Verification

```bash
cd web && pnpm lint && pnpm test && pnpm build
```

Manually verify disclosure, private-endpoint opt-in, sanitized test errors, masked secrets, disabled states, and transcription regression behavior.

## Comments

Implemented additive Generation and Embedding settings, disclosure acknowledgement, private-network opt-in, independent readiness/tests, write-only keys, and public external-processing disclosure while preserving transcription controls.
